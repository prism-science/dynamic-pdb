"use client";

import { useEffect, useMemo, useState } from "react";

import Experiment from "@/app/components/Experiment";
import SequencePanel from "@/app/components/SequencePanel";
import { loadCoordinateFile } from "@/lib/coordinate-file-cache";
import type { CrystallographyView } from "@/lib/crystallography";
import {
  experimentView,
  type ExperimentEntry,
} from "@/lib/experiment";
import type { PolymerEntityView } from "@/lib/polymer-entities";
import { sequenceChains } from "@/lib/sequence-tracks";
import { readStructure } from "@/lib/structure-tracks";

export function CoordinateExperiment({
  entry,
  crystallography,
  url,
}: {
  entry: ExperimentEntry;
  crystallography: CrystallographyView | null;
  url: string | null;
}) {
  const text = useCoordinateText(url);
  const view = useMemo(
    () => experimentView(entry, crystallography, text),
    [entry, crystallography, text],
  );
  return view ? <Experiment view={view} /> : null;
}

export function CoordinateSequencePanel({
  entities,
  url,
}: {
  entities: PolymerEntityView[];
  url: string | null;
}) {
  const text = useCoordinateText(url);
  const coordinates = useMemo(() => {
    if (text === null) {
      return [];
    }
    try {
      return readStructure(text);
    } catch (error) {
      console.error("parse model coordinates failed", url, error);
      return [];
    }
  }, [text, url]);
  const chains = useMemo(
    () => sequenceChains(entities, coordinates),
    [entities, coordinates],
  );
  return <SequencePanel chains={chains} />;
}

function useCoordinateText(url: string | null): string | null {
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

  return loaded?.url === url ? loaded.text : null;
}
