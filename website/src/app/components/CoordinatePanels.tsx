"use client";

import { useEffect, useMemo, useRef, useState } from "react";

import Experiment from "@/app/components/Experiment";
import SequencePanel from "@/app/components/SequencePanel";
import { loadCoordinateFile } from "@/lib/coordinate-file-cache";
import type { CrystallographyView } from "@/lib/crystallography";
import {
  experimentView,
  type ExperimentEntry,
} from "@/lib/experiment";
import type { OverlayModel } from "@/lib/model-overlays";
import type { PolymerEntityView } from "@/lib/polymer-entities";
import type { StructureKind } from "@/lib/structureKind";
import {
  sequenceChains,
  type OtherModelCoordinates,
} from "@/lib/sequence-tracks";
import { readStructure } from "@/lib/structure-tracks";

/**
 * How many of the entry's other models we read without being asked.
 *
 * Model-to-model disagreement still requires each coordinate file to be
 * downloaded and parsed in the browser.
 * Four is about where a comparison stops being readable anyway -- past that
 * the rows are a wall -- so the ceiling costs nothing a reader would miss, and
 * it stops an entry with thirty models from quietly pulling thirty files.
 */
const MAX_COMPARED_MODELS = 4;

/**
 * ...and how much text we will pull for them altogether.
 *
 * Counted after the fact, from the characters each file turned out to hold: a
 * content-length would need a request of its own, and one wasted download is
 * cheaper than one wasted round trip per model. What it buys is that a single
 * enormous ensemble stops the ones behind it rather than the page loading a
 * quarter of a gigabyte to draw a row.
 */
const COMPARE_BUDGET_CHARS = 48 * 1024 * 1024;

export function CoordinateExperiment({
  entry,
  crystallography,
  url,
}: {
  entry: ExperimentEntry;
  crystallography: CrystallographyView | null;
  url: string | null;
}) {
  const { text } = useCoordinateText(url);
  const view = useMemo(
    () => experimentView(entry, crystallography, text),
    [entry, crystallography, text],
  );
  return view ? <Experiment view={view} /> : null;
}

export function CoordinateSequencePanel({
  entities,
  url,
  kind,
  others = [],
}: {
  entities: PolymerEntityView[];
  url: string | null;
  /** The format of that file. Only mmCIF can be read here. */
  kind?: StructureKind | null;
  /** The entry's other models, for the per-residue comparison. */
  others?: OverlayModel[];
}) {
  // A PDB-format model is not downloaded at all. Nothing on this tab can be
  // read out of one -- the rows are drawn against label_seq_id, which the
  // format has no notion of -- and pulling several megabytes to parse nothing
  // out of them is worse than saying so.
  const baseReadable = kind === "mmcif";
  const { text, loading } = useCoordinateText(baseReadable ? url : null);
  const coordinates = useMemo(() => readChains(text, url), [text, url]);

  // Only mmCIF: the residue rows are read out of named mmCIF categories, and a
  // PDB-format file yields none of them. Counted as unread rather than left
  // out silently, so the panel can say the comparison is partial.
  const readable = useMemo(
    () => others.filter((model) => model.kind === "mmcif"),
    [others],
  );
  const wanted = readable.slice(0, MAX_COMPARED_MODELS);
  const comparison = useOtherModels(wanted);

  const chains = useMemo(
    () => sequenceChains(entities, coordinates, comparison.loaded),
    [entities, coordinates, comparison.loaded],
  );

  return (
    <SequencePanel
      chains={chains}
      comparison={{
        pending: comparison.pending,
        unread: others.length - wanted.length + comparison.unread,
        unreadableBase:
          url !== null && !baseReadable ? (kind ?? "unknown") : null,
        loadingBase: loading,
      }}
    />
  );
}

function readChains(text: string | null, url: string | null) {
  if (text === null) {
    return [];
  }
  try {
    return readStructure(text);
  } catch (error) {
    console.error("parse model coordinates failed", url, error);
    return [];
  }
}

/**
 * Read the other models' coordinates, one at a time.
 *
 * Sequential rather than all at once, and published after each file, so the
 * rows fill in as they arrive and a slow model does not hold up the ones
 * already read. One at a time also keeps the file cache -- which holds two
 * files -- from thrashing three large downloads against each other.
 */
function useOtherModels(models: OverlayModel[]): {
  loaded: OtherModelCoordinates[];
  /** Models still to read. */
  pending: number;
  /** Models we asked for and could not use. */
  unread: number;
} {
  const key = models.map((model) => model.url).join("|");
  const latest = useRef(models);
  latest.current = models;

  const [state, setState] = useState<{
    key: string;
    loaded: OtherModelCoordinates[];
    done: number;
    unread: number;
  }>({ key, loaded: [], done: 0, unread: 0 });

  useEffect(() => {
    const wanted = latest.current;
    if (wanted.length === 0) {
      setState({ key, loaded: [], done: 0, unread: 0 });
      return;
    }

    let active = true;
    const controller = new AbortController();
    setState({ key, loaded: [], done: 0, unread: 0 });

    void (async () => {
      const loaded: OtherModelCoordinates[] = [];
      let budget = COMPARE_BUDGET_CHARS;
      let unread = 0;

      for (let index = 0; index < wanted.length; index += 1) {
        const model = wanted[index];
        if (budget <= 0) {
          unread += wanted.length - index;
          break;
        }
        const text = await loadCoordinateFile(model.url, controller.signal);
        if (!active) {
          return;
        }
        if (text === null) {
          unread += 1;
        } else {
          budget -= text.length;
          const chains = readChains(text, model.url);
          if (chains.length === 0) {
            unread += 1;
          } else {
            loaded.push({
              modelId: model.modelId,
              title: model.title,
              chains,
            });
          }
        }
        setState({ key, loaded: [...loaded], done: index + 1, unread });
      }
      if (active) {
        setState({ key, loaded: [...loaded], done: wanted.length, unread });
      }
    })();

    return () => {
      active = false;
      controller.abort();
    };
    // Keyed by the files themselves: the array is rebuilt every render, and
    // re-running this effect means re-downloading everything.
  }, [key]);

  if (state.key !== key) {
    return { loaded: [], pending: models.length, unread: 0 };
  }
  return {
    loaded: state.loaded,
    pending: models.length - state.done,
    unread: state.unread,
  };
}

/**
 * One coordinate file, and whether it is still on its way.
 *
 * The two are told apart because they look the same in the data and not at all
 * the same on screen: a file that has not arrived yet wants a placeholder, and
 * a file that arrived empty wants an explanation.
 */
function useCoordinateText(url: string | null): {
  text: string | null;
  loading: boolean;
} {
  const [loaded, setLoaded] = useState<{
    url: string;
    text: string | null;
  } | null>(null);

  useEffect(() => {
    if (!url) {
      return;
    }
    let active = true;
    const controller = new AbortController();
    void loadCoordinateFile(url, controller.signal).then((text) => {
      if (active) {
        setLoaded({ url, text });
      }
    });
    return () => {
      active = false;
      controller.abort();
    };
  }, [url]);

  const settled = loaded?.url === url;
  return {
    text: settled ? loaded.text : null,
    loading: url !== null && !settled,
  };
}
