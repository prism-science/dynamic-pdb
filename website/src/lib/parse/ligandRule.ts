import { cifCategory, loopValue, type CifLoop } from "./cif";
import { CCD_CATEGORIES } from "./ccdCategories.gen";
import { CIF_REFERENCE } from "./cifReference.gen";
import {
  AMBIGUOUS_COMPOUND_REASON,
  DRAFT_SOLVENT_REASON,
  NO_METALC_ROWS_REASON,
  OUTSIDE_CHAIN_REASON,
  SINGLE_ATOM_REASON,
  UNLINKED_METAL_REASON,
  WATER_ONLY_METAL_REASON,
} from "./reviewReasons";

// The ligand rule, as the backend parser applies it (lib/cif/sorting.go and
// lib/cif/annotations.go): where each compound instance of a file goes
// (ligands, cofactors, or left out) and which rule put it there. The parity
// test (parity-golden.json) fails when the two drift.

/** One compound flagged for review: the shape of the backend's
 *  `extensions.compound_review` entries. */
export type CompoundReview = {
  list: "ligands" | "cofactors";
  id: string;
  reason: string;
};

/** The rule that placed a compound, as the decision record stores it. */
export type CompoundRule =
  | "list"
  | "metalc"
  | "branched"
  | "macrolide"
  | "ccd_type"
  | "depositor_flag"
  | "covalent_map"
  | "ihm_probe";

export type CompoundSource = { rule: CompoundRule; list_version: string };

/** Mirrors cif.CompoundSources: per placement, the rule that put each CCD ID
 *  there. Water and polymer residues are never recorded. */
export type CompoundSources = {
  ligands: Record<string, CompoundSource>;
  cofactors: Record<string, CompoundSource>;
  excluded: Record<string, CompoundSource>;
};

export type GlycanDescriptor = {
  descriptor: string;
  type: string;
  program?: string;
  program_version?: string;
};

/** One branched entity: its sugars and its WURCS descriptors. */
export type Glycan = {
  entity_id: string;
  compound_ids: string[];
  descriptors: GlycanDescriptor[];
};

/** What the ligand rule gives a file. */
export type SortedCompounds = {
  ligands: string[];
  cofactors: string[];
  reviews: CompoundReview[];
  sources: CompoundSources;
};

/* -------------------------------------------------------- reference -- */

// Water is always dropped: every crystal has it and nobody means it when they
// say "the ligand".
export const WATER_COMPOUNDS = new Set(["HOH", "DOD", "WAT", "H2O"]);

// The reference lists are generated from the JSON files in lib/cif
// (npm run gen:cif-reference), so the preview and the backend parser cannot
// disagree about them. While any list is a draft (`approved: false` in the
// JSON), only water is dropped; other listed solvents are kept and flagged for
// review.
const REFERENCE_LISTS_APPROVED: boolean = CIF_REFERENCE.approved;
const SOLVENT_COMPOUNDS = new Set<string>(CIF_REFERENCE.solvents);
const AMBIGUOUS_COMPOUNDS = new Set<string>(CIF_REFERENCE.ambiguous);
const KEEP_WHEN_METAL_LINKED = new Set<string>(CIF_REFERENCE.keepWhenMetalLinked);
const COFACTOR_COMPOUNDS = new Set<string>(CIF_REFERENCE.cofactors);
const METAL_IONS = new Set<string>(CIF_REFERENCE.metalIons);
const METAL_ELEMENTS = new Set<string>(CIF_REFERENCE.metalElements);
const NONMETAL_ELEMENTS = new Set<string>(CIF_REFERENCE.nonmetalElements);
const COVALENT_COFACTORS = new Map<string, string>(Object.entries(CIF_REFERENCE.covalentCofactors));

type CcdCategory = "peptide" | "nucleotide" | "saccharide" | "nonPolymer";

const CCD_CATEGORY_OF = new Map<string, CcdCategory>();
for (const category of ["peptide", "nucleotide", "saccharide"] as const) {
  for (const id of CCD_CATEGORIES[category]) {
    CCD_CATEGORY_OF.set(id, category);
  }
}

type CompoundKind =
  | "ligand"
  | "cofactor"
  | "metal"
  | "solvent"
  | "draftSolvent"
  | "ambiguous"
  | "solventUnlessMetalLinked";

// Mirrors Reference.kind in lib/cif/reference.go.
function compoundKind(compound: string): CompoundKind {
  if (WATER_COMPOUNDS.has(compound)) {
    return "solvent";
  }
  if (SOLVENT_COMPOUNDS.has(compound)) {
    if (!REFERENCE_LISTS_APPROVED) {
      return "draftSolvent";
    }
    return KEEP_WHEN_METAL_LINKED.has(compound) ? "solventUnlessMetalLinked" : "solvent";
  }
  if (AMBIGUOUS_COMPOUNDS.has(compound)) {
    return "ambiguous";
  }
  if (METAL_IONS.has(compound)) {
    return "metal";
  }
  if (COFACTOR_COMPOUNDS.has(compound)) {
    return "cofactor";
  }
  return "ligand";
}

/** Whether a compound is on the metal ion list, for reading PDB LINK records. */
export function isMetalIon(compound: string): boolean {
  return compoundKind(compound) === "metal";
}

type ElementClass = "metal" | "nonmetal" | "unknown";

// Mirrors Reference.elementClass in lib/cif/reference.go: a symbol on neither
// list (empty, or X for an unknown atom) is unknown.
function elementClass(symbol: string): ElementClass {
  const element = symbol.trim().toUpperCase();
  if (METAL_ELEMENTS.has(element)) {
    return "metal";
  }
  return NONMETAL_ELEMENTS.has(element) ? "nonmetal" : "unknown";
}

/** Mirrors cif.CCDTypeCategory: a chem_comp.type value, ignoring case. Null
 *  for a type it does not know. */
function ccdTypeCategory(ccdType: string): CcdCategory | null {
  switch (ccdType.trim().toLowerCase()) {
    case "d-beta-peptide, c-gamma linking":
    case "d-gamma-peptide, c-delta linking":
    case "d-peptide cooh carboxy terminus":
    case "d-peptide nh3 amino terminus":
    case "d-peptide linking":
    case "l-beta-peptide, c-gamma linking":
    case "l-gamma-peptide, c-delta linking":
    case "l-peptide cooh carboxy terminus":
    case "l-peptide nh3 amino terminus":
    case "l-peptide linking":
    case "peptide linking":
    case "peptide-like":
      return "peptide";
    case "dna oh 3 prime terminus":
    case "dna oh 5 prime terminus":
    case "dna linking":
    case "l-dna linking":
    case "l-rna linking":
    case "rna oh 3 prime terminus":
    case "rna oh 5 prime terminus":
    case "rna linking":
      return "nucleotide";
    case "d-saccharide":
    case "d-saccharide, alpha linking":
    case "d-saccharide, beta linking":
    case "l-saccharide":
    case "l-saccharide, alpha linking":
    case "l-saccharide, beta linking":
    case "saccharide":
      return "saccharide";
    case "non-polymer":
    case "other":
      return "nonPolymer";
    default:
      return null;
  }
}

/* ------------------------------------------------------- instances -- */

/** One placed copy of a non-polymer compound. Mirrors compoundInstance in
 *  lib/cif/extract.go: an instance read from `_pdbx_entity_nonpoly` alone has
 *  no asym ID and no atoms. */
export type CompoundInstance = {
  compound: string;
  asym: string;
  seq: string;
  ins: string;
};

export function instanceKey(instance: CompoundInstance): string {
  return [instance.compound, instance.asym, instance.seq, instance.ins].join("\u0000");
}

/** One HETATM row as the rule needs it. */
export type HeteroAtom = {
  name: string;
  /** Upper-case element symbol. */
  element: string;
  /** label_entity_id; "" in PDB-format files. */
  entityId: string;
  /** Set when an mmCIF row has a label_seq_id: the residue sits within a
   *  chain. A PDB-format file decides chain positions after the read. */
  inChain: boolean;
  /** Set for a row of any model after the first. */
  laterModel: boolean;
};

/** Mirrors chainPosition in lib/cif/tally.go: what a file says about a
 *  residue's place in a polymer chain. "member": a PDB-format MODRES record, or
 *  a name in the chain's SEQRES at a position in its polymer block (polymer
 *  whatever its CCD type). "spanOnly": within a chain, but nothing names it as
 *  a chain residue (an mmCIF label_seq_id, or a PDB chain without SEQRES); only
 *  a peptide or nucleotide counts as in the chain. */
export type ChainPosition = "member" | "spanOnly" | "none";

/** What the coordinates say about one instance: its atom count (0 when
 *  unknown) and the facts of its first atom. */
export type InstanceFacts = {
  instance: CompoundInstance;
  atomCount: number;
  element: string;
  entityId: string;
  chain: ChainPosition;
  laterModel: boolean;
};

/** The coordinates' view of the non-polymer compounds: the distinct atoms seen
 *  per instance with its first atom's facts, and the atom count a PDB `HET`
 *  record declares. Mirrors coordinateTally in lib/cif/tally.go. */
export class CompoundTally {
  readonly instances = new Map<string, { instance: CompoundInstance; atoms: Set<string>; first: HeteroAtom }>();
  readonly declared = new Map<string, { instance: CompoundInstance; count: number; entityId: string }>();

  /** Rows of a later model add an instance the first model lacks, never atoms
   *  to one it has. */
  addAtom(instance: CompoundInstance, atom: HeteroAtom): void {
    const key = instanceKey(instance);
    const entry = this.instances.get(key);
    if (!entry) {
      this.instances.set(key, { instance, atoms: new Set([atom.name]), first: atom });
      return;
    }
    if (atom.laterModel && !entry.first.laterModel) {
      return;
    }
    entry.atoms.add(atom.name);
  }

  declare(instance: CompoundInstance, count: number, entityId: string): void {
    this.declared.set(instanceKey(instance), { instance, count, entityId });
  }

  /** Instance → facts. A declared atom count wins. */
  facts(): Map<string, InstanceFacts> {
    const facts = new Map<string, InstanceFacts>();
    for (const [key, entry] of this.instances) {
      facts.set(key, {
        instance: entry.instance,
        atomCount: entry.atoms.size,
        element: entry.first.element,
        entityId: entry.first.entityId,
        chain: entry.first.inChain ? "spanOnly" : "none",
        laterModel: entry.first.laterModel,
      });
    }
    for (const [key, entry] of this.declared) {
      const observed = facts.get(key);
      if (entry.count > 0 && observed) {
        facts.set(key, { ...observed, atomCount: entry.count });
      } else if (entry.count > 0) {
        facts.set(key, emptyFacts(entry.instance, entry.count, entry.entityId));
      } else if (!observed) {
        facts.set(key, emptyFacts(entry.instance, 0, entry.entityId));
      }
    }
    return facts;
  }
}

function emptyFacts(instance: CompoundInstance, atomCount: number, entityId: string): InstanceFacts {
  return { instance, atomCount, element: "", entityId, chain: "none", laterModel: false };
}

/** What the file's `metalc` connection rows say about metal ions. */
export type MetalcLinks = {
  /** False when the file has no metalc rows at all (every PDB file here). */
  hasRows: boolean;
  /** The two partners of each metalc row. */
  pairs: Array<[CompoundInstance, CompoundInstance]>;
};

export const NO_METALC_ROWS: MetalcLinks = { hasRows: false, pairs: [] };

/* ----------------------------------------------------- annotations -- */

type EntityType = "polymer" | "nonPolymer" | "branched" | "macrolide" | "water" | "unknownType";
type FeatureType = "subject" | "noFunctionalRole" | "other" | "unknownType";
type DepositorFlag = { compound: string; asym: string; seq: string; feature: FeatureType };

/** Mirrors fileAnnotations in lib/cif/annotations.go: what the categories
 *  besides `_atom_site` say about the compounds. */
export type FileAnnotations = {
  entitiesRead: boolean;
  entityTypes: Map<string, EntityType>;
  chemCompTypes: Map<string, string>;
  /** The depositor flags in file order; firstFlag is the position in flags of
   *  the first one with each compound, asym and residue number. */
  flags: DepositorFlag[];
  firstFlag: Map<string, number>;
  modifiedResidues: string[];
  /** Residue names of every chain's SEQRES records (PDB format): only a
   *  peptide or nucleotide named there is suspicious outside a chain. */
  sequenceNames: Set<string>;
  glycans: Glycan[];
  ihm: boolean;
  probeEntities: Set<string>;
  entityReview: Map<string, string>;
  compoundReview: Map<string, string>;
};

/** A PDB-format file says nothing beyond its records. */
export function pdbAnnotations(): FileAnnotations {
  return {
    entitiesRead: false,
    entityTypes: new Map(),
    chemCompTypes: new Map(),
    flags: [],
    firstFlag: new Map(),
    modifiedResidues: [],
    sequenceNames: new Set(),
    glycans: [],
    ihm: false,
    probeEntities: new Set(),
    entityReview: new Map(),
    compoundReview: new Map(),
  };
}

function value(loop: CifLoop, row: string[], name: string): string {
  return loopValue(loop, row, name) ?? "";
}

/** Reads the categories the rule uses. atomSiteIHM is set when the coordinate
 *  loop has an ihm_model_id column. */
export function readAnnotations(text: string, atomSiteIHM: boolean): FileAnnotations {
  const annotations = pdbAnnotations();

  const entities = cifCategory(text, "_entity");
  if (entities) {
    annotations.entitiesRead = true;
    for (const row of entities.rows) {
      const id = value(entities, row, "id");
      if (!id) {
        continue;
      }
      const type = value(entities, row, "type");
      switch (type.toLowerCase()) {
        case "polymer":
          annotations.entityTypes.set(id, "polymer");
          break;
        case "non-polymer":
          annotations.entityTypes.set(id, "nonPolymer");
          break;
        case "branched":
          annotations.entityTypes.set(id, "branched");
          break;
        case "macrolide":
          annotations.entityTypes.set(id, "macrolide");
          break;
        case "water":
          annotations.entityTypes.set(id, "water");
          break;
        default:
          annotations.entityTypes.set(id, "unknownType");
          annotations.entityReview.set(id, `unknown entity type ${JSON.stringify(type)}: confirm where this compound belongs`);
      }
    }
  }

  const chemComps = cifCategory(text, "_chem_comp");
  for (const row of chemComps?.rows ?? []) {
    const id = value(chemComps!, row, "id").toUpperCase();
    if (id) {
      annotations.chemCompTypes.set(id, value(chemComps!, row, "type"));
    }
  }

  const features = cifCategory(text, "_pdbx_entity_instance_feature");
  for (const row of features?.rows ?? []) {
    const compound = value(features!, row, "comp_id").toUpperCase();
    if (!compound) {
      continue;
    }
    const type = value(features!, row, "feature_type");
    let feature: FeatureType;
    switch (type.toUpperCase()) {
      case "SUBJECT OF INVESTIGATION":
        feature = "subject";
        break;
      case "NO FUNCTIONAL ROLE":
        feature = "noFunctionalRole";
        break;
      case "OTHER":
        feature = "other";
        break;
      default:
        feature = "unknownType";
        annotations.compoundReview.set(compound, `unknown depositor flag ${JSON.stringify(type)}: confirm where this compound belongs`);
    }
    const flag: DepositorFlag = {
      compound,
      asym: value(features!, row, "asym_id"),
      seq: value(features!, row, "auth_seq_num"),
      feature,
    };
    const key = flagKey(flag.compound, flag.asym, flag.seq);
    if (!annotations.firstFlag.has(key)) {
      annotations.firstFlag.set(key, annotations.flags.length);
    }
    annotations.flags.push(flag);
  }

  const modified = cifCategory(text, "_pdbx_struct_mod_residue");
  const modifiedSeen = new Set<string>();
  for (const row of modified?.rows ?? []) {
    const id = value(modified!, row, "label_comp_id").toUpperCase();
    if (id && !modifiedSeen.has(id)) {
      modifiedSeen.add(id);
      annotations.modifiedResidues.push(id);
    }
  }

  if ([...annotations.entityTypes.values()].includes("branched")) {
    annotations.glycans = readGlycans(text, annotations);
  }

  annotations.ihm = atomSiteIHM || cifCategory(text, "_ihm_model_list") !== null;
  if (annotations.ihm) {
    const probes = cifCategory(text, "_ihm_ligand_probe");
    for (const row of probes?.rows ?? []) {
      const entity = value(probes!, row, "entity_id");
      if (entity) {
        annotations.probeEntities.add(entity);
      }
    }
  }
  return annotations;
}

/** One record per branched entity: its sugars from `_pdbx_entity_branch_list`
 *  and its WURCS descriptors. Other known descriptor types are not kept; an
 *  unknown type flags the entity's compounds. */
function readGlycans(text: string, annotations: FileAnnotations): Glycan[] {
  const byEntity = new Map<string, Glycan>();
  for (const [id, type] of annotations.entityTypes) {
    if (type === "branched") {
      byEntity.set(id, { entity_id: id, compound_ids: [], descriptors: [] });
    }
  }
  const members = cifCategory(text, "_pdbx_entity_branch_list");
  const seen = new Set<string>();
  for (const row of members?.rows ?? []) {
    const entityId = value(members!, row, "entity_id");
    const glycan = byEntity.get(entityId);
    const compound = value(members!, row, "comp_id").toUpperCase();
    const member = `${entityId}\u0000${compound}`;
    if (glycan && compound && !seen.has(member)) {
      seen.add(member);
      glycan.compound_ids.push(compound);
    }
  }
  const descriptors = cifCategory(text, "_pdbx_entity_branch_descriptor");
  for (const row of descriptors?.rows ?? []) {
    const entityId = value(descriptors!, row, "entity_id");
    const glycan = byEntity.get(entityId);
    if (!glycan) {
      continue;
    }
    const type = value(descriptors!, row, "type");
    switch (type.toUpperCase()) {
      case "WURCS": {
        const descriptor: GlycanDescriptor = { descriptor: value(descriptors!, row, "descriptor"), type };
        const program = value(descriptors!, row, "program");
        const programVersion = value(descriptors!, row, "program_version");
        if (program) {
          descriptor.program = program;
        }
        if (programVersion) {
          descriptor.program_version = programVersion;
        }
        glycan.descriptors.push(descriptor);
        break;
      }
      case "LINUCS":
      case "GLYCAM CONDENSED SEQUENCE":
      case "GLYCAM CONDENSED CORE SEQUENCE":
        // Known types the researchers did not ask for: WURCS alone is kept.
        break;
      default:
        annotations.entityReview.set(entityId, `unknown glycan descriptor type ${JSON.stringify(type)}: confirm the glycan`);
    }
  }
  return [...byEntity.values()]
    .sort((left, right) => compareEntityIds(left.entity_id, right.entity_id))
    .map((glycan) => ({ ...glycan, compound_ids: [...glycan.compound_ids].sort(compareText) }));
}

/** Entity IDs as numbers when both are, as archive files number them. */
function compareEntityIds(left: string, right: string): number {
  const digits = /^[0-9]+$/;
  if (left.length !== right.length && digits.test(left) && digits.test(right)) {
    return left.length - right.length;
  }
  return compareText(left, right);
}

/* ------------------------------------------------------------- rule -- */

/** Mirrors rulePrecedence: an ID placed in one group twice keeps the more
 *  specific rule. */
function rulePrecedence(rule: CompoundRule): number {
  switch (rule) {
    case "depositor_flag":
      return 8;
    case "ihm_probe":
      return 7;
    case "covalent_map":
      return 6;
    case "branched":
      return 5;
    case "macrolide":
      return 4;
    case "ccd_type":
      return 3;
    case "metalc":
      return 2;
    case "list":
      return 1;
    default:
      throw new Error(`unknown compound rule: ${String(rule satisfies never)}`);
  }
}

type Placement = "ligand" | "cofactor" | "excluded";
type Decision = { id: string; review: string | null; placement: Placement; rule: CompoundRule };
type Shape = "polymer" | "water" | "listed" | "branched" | "macrolide" | "outsideChain";

/** What happens to a metal's link partner, and so what the link means for the
 *  metal. Mirrors partnerFate in lib/cif/sorting.go: "dropped" (left out, so
 *  the link does not count), "kept" (a polymer residue or a kept compound),
 *  "water" (the metal may be linked only to water), "bindsMetal" (imidazole:
 *  the metal is linked and the partner kept). */
type PartnerFate = "dropped" | "kept" | "water" | "bindsMetal";

/** Mirrors metalPartners in lib/cif/sorting.go: which metal instances the
 *  metalc rows link to a kept partner or only to water, and which instances of
 *  keep-when-metal-linked compounds (imidazole) they bind to a metal. An
 *  instance without a location is looked up by compound ID. */
class MetalPartners {
  private readonly linked = new Set<string>();
  private readonly water = new Set<string>();
  private readonly bound = new Set<string>();

  constructor(metalcLinks: MetalcLinks, fateOf: (partner: CompoundInstance) => PartnerFate) {
    for (const [first, second] of metalcLinks.pairs) {
      this.add(first, second, fateOf);
      this.add(second, first, fateOf);
    }
  }

  /** One side of a metalc row: what the partner means for the metal. */
  private add(metal: CompoundInstance, partner: CompoundInstance, fateOf: (partner: CompoundInstance) => PartnerFate): void {
    if (compoundKind(metal.compound) !== "metal" || partner.compound === "") {
      return;
    }
    const fate = fateOf(partner);
    switch (fate) {
      case "dropped":
        return;
      case "kept":
        this.mark(this.linked, metal);
        return;
      case "water":
        this.mark(this.water, metal);
        return;
      case "bindsMetal":
        this.mark(this.bound, partner);
        this.mark(this.linked, metal);
        return;
      default:
        throw new Error(`unknown partner fate for ${partner.compound}: ${String(fate satisfies never)}`);
    }
  }

  private mark(set: Set<string>, instance: CompoundInstance): void {
    set.add(instanceKey(instance));
    set.add(instance.compound);
  }

  private has(set: Set<string>, instance: CompoundInstance): boolean {
    return set.has(instance.asym !== "" ? instanceKey(instance) : instance.compound);
  }

  state(metal: CompoundInstance): "linked" | "waterOnly" | "unlinked" {
    if (this.has(this.linked, metal)) {
      return "linked";
    }
    return this.has(this.water, metal) ? "waterOnly" : "unlinked";
  }

  boundToMetal(instance: CompoundInstance): boolean {
    return this.has(this.bound, instance);
  }
}

/**
 * The same rule as the backend parser (classifyCompounds in
 * lib/cif/sorting.go), per instance:
 *
 *  1. Water is dropped and polymer residues (MSE in a chain) are not
 *     compounds. The entity type decides; without one, the CCD type and the
 *     chain position.
 *  2. The depositor's flag: SUBJECT OF INVESTIGATION keeps it as a ligand,
 *     NO FUNCTIONAL ROLE leaves it out, whatever the lists say.
 *  3. An integrative model's probes are left out.
 *  4. Glycans (branched entities) and macrolides are ligands.
 *  5. Everything else goes by the reference lists and the metal rule.
 *
 * A modified residue that is a covalently bound cofactor (LLP) adds that
 * cofactor (PLP). An integrative file's compounds come from every model.
 */
export function sortCompounds(
  instances: Map<string, InstanceFacts>,
  metalcLinks: MetalcLinks,
  annotations: FileAnnotations,
): SortedCompounds {
  const partners = new MetalPartners(metalcLinks, (partner) => partnerFate(partner, instances, annotations));
  const decisions: Decision[] = [];
  for (const facts of orderedInstances(instances, annotations.ihm)) {
    const placed = decide(facts, partners, metalcLinks.hasRows, annotations);
    if (placed) {
      decisions.push(placed);
    }
  }
  for (const residue of annotations.modifiedResidues) {
    const cofactor = COVALENT_COFACTORS.get(residue);
    if (cofactor) {
      decisions.push({ id: cofactor, review: null, placement: "cofactor", rule: "covalent_map" });
    }
  }
  return collectDecisions(decisions);
}

/** Mirrors partnerFate in lib/cif/sorting.go: places a metal's link partner
 *  by the steps that place it as a compound, so a link counts only when the
 *  partner stays in the entry. A partner with coordinates goes by its shape,
 *  the depositor's flag and the probe exclusion first; a polymer residue of the
 *  ATOM records, and a partner left to the lists, go by the lists. */
function partnerFate(
  partner: CompoundInstance,
  instances: Map<string, InstanceFacts>,
  annotations: FileAnnotations,
): PartnerFate {
  if (WATER_COMPOUNDS.has(partner.compound)) {
    return "water";
  }
  const facts = instances.get(instanceKey(partner));
  const annotated = facts ? annotatedPartnerFate(facts, annotations) : null;
  if (annotated) {
    return annotated;
  }
  const kind = compoundKind(partner.compound);
  switch (kind) {
    case "solvent":
      return "dropped";
    case "solventUnlessMetalLinked":
      return "bindsMetal";
    case "ligand":
    case "cofactor":
    case "metal":
    case "ambiguous":
    case "draftSolvent":
      return "kept";
    default:
      throw new Error(`unknown compound kind for metal partner ${partner.compound}: ${String(kind satisfies never)}`);
  }
}

/** Steps 1 to 4 of the rule for a partner with coordinates; null when the
 *  lists decide. Mirrors annotatedPartnerFate in lib/cif/sorting.go. */
function annotatedPartnerFate(facts: InstanceFacts, annotations: FileAnnotations): PartnerFate | null {
  const id = facts.instance.compound;
  const [shape] = shapeOf(facts, annotations);
  switch (shape) {
    case "water":
      return "water";
    case "polymer":
      return "kept";
    case "branched":
    case "macrolide":
    case "outsideChain":
    case "listed":
      break;
    default:
      throw new Error(`unknown compound shape for ${id}: ${String(shape satisfies never)}`);
  }
  const flag = flagFor(annotations, facts.instance);
  if (flag) {
    switch (flag.feature) {
      case "subject":
        return "kept";
      case "noFunctionalRole":
        return "dropped";
      case "other":
      case "unknownType":
        break;
      default:
        throw new Error(`unknown depositor flag for ${id}: ${String(flag.feature satisfies never)}`);
    }
  }
  if (annotations.probeEntities.has(facts.entityId)) {
    return "dropped";
  }
  switch (shape) {
    case "branched":
    case "macrolide":
    case "outsideChain":
      return "kept";
    case "listed":
      return null;
    default:
      throw new Error(`unknown compound shape for ${id}: ${String(shape satisfies never)}`);
  }
}

/** Instances sorted by compound and location. A compound seen only in
 *  `_pdbx_entity_nonpoly` is skipped when the coordinates placed instances of
 *  it, and instances of later models count only for an integrative file. */
function orderedInstances(instances: Map<string, InstanceFacts>, allModels: boolean): InstanceFacts[] {
  const included = [...instances.values()].filter((facts) => allModels || !facts.laterModel);
  const placed = new Set(included.filter(({ instance }) => instance.asym !== "").map(({ instance }) => instance.compound));
  return included
    .filter(({ instance }) => instance.asym !== "" || !placed.has(instance.compound))
    .sort(({ instance: left }, { instance: right }) =>
      compareText(left.compound, right.compound) ||
      compareText(left.asym, right.asym) ||
      compareText(left.seq, right.seq) ||
      compareText(left.ins, right.ins),
    );
}

/** Places one instance; null when it is not a compound. */
function decide(
  facts: InstanceFacts,
  partners: MetalPartners,
  hasMetalcRows: boolean,
  annotations: FileAnnotations,
): Decision | null {
  const { instance } = facts;
  const id = instance.compound;
  if (WATER_COMPOUNDS.has(id)) {
    return null;
  }
  const [shape, shapeReview] = shapeOf(facts, annotations);
  const review =
    annotations.compoundReview.get(id) ?? annotations.entityReview.get(facts.entityId) ?? shapeReview;
  if (shape === "water" || shape === "polymer") {
    return null;
  }
  const placed = placeByShape(facts, shape, partners, hasMetalcRows, annotations);
  if (review && placed.placement !== "excluded" && placed.review === null) {
    placed.review = review;
  }
  return placed;
}

function flagKey(compound: string, asym: string, seq: string): string {
  return [compound, asym, seq].join("\u0000");
}

/** The first depositor flag, in file order, that matches an instance: one that
 *  names its compound and either names its asym and residue number or leaves
 *  them open. An instance without a location matches only flags without one.
 *  The four keys an instance can match are looked up, so the cost does not
 *  grow with the number of flags. */
function flagFor(annotations: FileAnnotations, instance: CompoundInstance): DepositorFlag | undefined {
  let first = -1;
  for (const key of [
    flagKey(instance.compound, instance.asym, instance.seq),
    flagKey(instance.compound, instance.asym, ""),
    flagKey(instance.compound, "", instance.seq),
    flagKey(instance.compound, "", ""),
  ]) {
    const position = annotations.firstFlag.get(key);
    if (position !== undefined && (first < 0 || position < first)) {
      first = position;
    }
  }
  return first < 0 ? undefined : annotations.flags[first];
}

/** Steps 2 to 5 of the rule. */
function placeByShape(
  facts: InstanceFacts,
  shape: Shape,
  partners: MetalPartners,
  hasMetalcRows: boolean,
  annotations: FileAnnotations,
): Decision {
  const { instance } = facts;
  const id = instance.compound;
  const flag = flagFor(annotations, instance);
  if (flag) {
    switch (flag.feature) {
      case "subject":
        return { id, review: null, placement: "ligand", rule: "depositor_flag" };
      case "noFunctionalRole":
        return { id, review: null, placement: "excluded", rule: "depositor_flag" };
      case "other":
      case "unknownType":
        break;
      default:
        throw new Error(`unknown depositor flag for ${id}: ${String(flag.feature satisfies never)}`);
    }
  }
  if (annotations.probeEntities.has(facts.entityId)) {
    return { id, review: null, placement: "excluded", rule: "ihm_probe" };
  }
  switch (shape) {
    case "branched":
      return { id, review: null, placement: "ligand", rule: "branched" };
    case "macrolide":
      return { id, review: null, placement: "ligand", rule: "macrolide" };
    case "outsideChain":
      return { id, review: OUTSIDE_CHAIN_REASON, placement: "ligand", rule: "ccd_type" };
    case "listed":
      return byLists(facts, partners, hasMetalcRows);
    case "polymer":
    case "water":
      throw new Error(`${id} is not a compound`);
    default:
      throw new Error(`unknown compound shape for ${id}: ${String(shape satisfies never)}`);
  }
}

/** Mirrors shapeOf: the entity type. An instance whose entity the file does
 *  not list (a PDB-format file, or an mmCIF file without `_entity`) is polymer
 *  when the file names it as a chain residue; otherwise its CCD type decides
 *  (the file's own `_chem_comp.type`, then the CCD table). A peptide or
 *  nucleotide within a chain counts as polymer too. Outside any chain, it is a
 *  ligand to confirm when its name is in the file's SEQRES records; otherwise
 *  the lists sort it (GDP, typed "RNA linking" in the CCD). */
function shapeOf(facts: InstanceFacts, annotations: FileAnnotations): [Shape, string | null] {
  const entity = annotations.entitiesRead && facts.entityId ? annotations.entityTypes.get(facts.entityId) : undefined;
  switch (entity) {
    case "water":
      return ["water", null];
    case "polymer":
      return ["polymer", null];
    case "nonPolymer":
    case "unknownType":
      return ["listed", null];
    case "branched":
      return ["branched", null];
    case "macrolide":
      return ["macrolide", null];
    case undefined:
      break;
    default:
      throw new Error(`unknown entity type for ${facts.instance.compound}: ${String(entity satisfies never)}`);
  }
  switch (facts.chain) {
    case "member":
      return ["polymer", null];
    case "spanOnly":
    case "none":
      break;
    default:
      throw new Error(`unknown chain position for ${facts.instance.compound}: ${String(facts.chain satisfies never)}`);
  }
  const [category, review] = categoryOf(facts.instance.compound, annotations);
  switch (category) {
    case "peptide":
    case "nucleotide":
      if (facts.chain === "spanOnly") {
        return ["polymer", null];
      }
      return [annotations.sequenceNames.has(facts.instance.compound) ? "outsideChain" : "listed", review];
    case "saccharide":
      return ["branched", review];
    case "nonPolymer":
      return ["listed", review];
    default:
      throw new Error(`unknown CCD category for ${facts.instance.compound}: ${String(category satisfies never)}`);
  }
}

/** Mirrors categoryOf: the file's own `_chem_comp.type` when it has one, else
 *  the CCD table. An unknown type is sorted by the lists and flagged. */
function categoryOf(id: string, annotations: FileAnnotations): [CcdCategory, string | null] {
  const fileType = annotations.chemCompTypes.get(id);
  if (fileType) {
    const category = ccdTypeCategory(fileType);
    if (category) {
      return [category, null];
    }
    return ["nonPolymer", `unknown chemical component type ${JSON.stringify(fileType)}: confirm where this compound belongs`];
  }
  return [CCD_CATEGORY_OF.get(id) ?? "nonPolymer", null];
}

/** Mirrors byLists: a non-polymer sorted by the reference lists and the metal rule. */
function byLists(facts: InstanceFacts, partners: MetalPartners, hasMetalcRows: boolean): Decision {
  const { instance } = facts;
  const id = instance.compound;
  const kind = compoundKind(id);
  switch (kind) {
    case "solvent":
      return { id, review: null, placement: "excluded", rule: "list" };
    case "solventUnlessMetalLinked":
      return partners.boundToMetal(instance)
        ? { id, review: null, placement: "ligand", rule: "metalc" }
        : { id, review: null, placement: "excluded", rule: "list" };
    case "draftSolvent":
      return { id, review: DRAFT_SOLVENT_REASON, placement: "ligand", rule: "list" };
    case "ambiguous":
      return { id, review: AMBIGUOUS_COMPOUND_REASON, placement: "ligand", rule: "list" };
    case "cofactor":
      return { id, review: null, placement: "cofactor", rule: "list" };
    case "ligand":
      return { id, review: singleAtomReview(facts.atomCount, facts.element), placement: "ligand", rule: "list" };
    case "metal": {
      const state = partners.state(instance);
      switch (state) {
        case "linked":
          return { id, review: null, placement: "cofactor", rule: "metalc" };
        case "waterOnly":
          return { id, review: WATER_ONLY_METAL_REASON, placement: "cofactor", rule: "metalc" };
        case "unlinked":
          return hasMetalcRows
            ? { id, review: UNLINKED_METAL_REASON, placement: "ligand", rule: "metalc" }
            : { id, review: NO_METALC_ROWS_REASON, placement: "cofactor", rule: "metalc" };
        default:
          throw new Error(`unknown metal link state for ${id}: ${String(state satisfies never)}`);
      }
    }
    default:
      throw new Error(`unknown compound kind for ${id}: ${String(kind satisfies never)}`);
  }
}

/** Mirrors ligandCompound: a single-atom instance no list names is flagged
 *  when its element is a metal or unknown. */
function singleAtomReview(atomCount: number, element: string): string | null {
  if (atomCount !== 1) {
    return null;
  }
  const kind = elementClass(element);
  switch (kind) {
    case "nonmetal":
      return null;
    case "metal":
    case "unknown":
      return SINGLE_ATOM_REASON;
    default:
      throw new Error(`unknown element class: ${String(kind satisfies never)}`);
  }
}

/** Mirrors collectDecisions: lists in compound ID order, a flagged entry
 *  winning over an unflagged one, and the record keeping the more specific
 *  rule. */
function collectDecisions(decisions: Decision[]): SortedCompounds {
  const ordered = [...decisions].sort((left, right) => compareText(left.id, right.id));
  const ligands = new CompoundList("ligands");
  const cofactors = new CompoundList("cofactors");
  const sources: CompoundSources = { ligands: {}, cofactors: {}, excluded: {} };
  for (const placed of ordered) {
    let group: Record<string, CompoundSource>;
    switch (placed.placement) {
      case "ligand":
        ligands.add(placed.id, placed.review);
        group = sources.ligands;
        break;
      case "cofactor":
        cofactors.add(placed.id, placed.review);
        group = sources.cofactors;
        break;
      case "excluded":
        group = sources.excluded;
        break;
      default:
        throw new Error(`unknown placement for ${placed.id}: ${String(placed.placement satisfies never)}`);
    }
    const current = group[placed.id];
    if (!current || rulePrecedence(placed.rule) > rulePrecedence(current.rule)) {
      group[placed.id] = { rule: placed.rule, list_version: CIF_REFERENCE.referenceVersion };
    }
  }
  return {
    ligands: ligands.ids(),
    cofactors: cofactors.ids(),
    reviews: [...ligands.reviews(), ...cofactors.reviews()],
    sources,
  };
}

export function compareText(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

/** One entry per compound ID; a flagged entry wins over an unflagged one so a
 *  review flag on any instance is never lost. */
class CompoundList {
  private readonly entries = new Map<string, string | null>();

  constructor(private readonly list: "ligands" | "cofactors") {}

  add(id: string, reviewReason: string | null): void {
    const existing = this.entries.get(id);
    if (existing === undefined || (existing === null && reviewReason !== null)) {
      this.entries.set(id, reviewReason);
    }
  }

  ids(): string[] {
    return [...this.entries.keys()];
  }

  reviews(): CompoundReview[] {
    const reviews: CompoundReview[] = [];
    for (const [id, reason] of this.entries) {
      if (reason !== null) {
        reviews.push({ list: this.list, id, reason });
      }
    }
    return reviews;
  }
}
