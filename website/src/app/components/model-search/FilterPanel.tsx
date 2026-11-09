"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useId, useState, useTransition, type FormEvent } from "react";

import {
  activeFilterCount,
  browseSearchHref,
  criteriaFromPanel,
  FILTER_GROUPS,
  MODEL_SEARCH_FILTERS,
  panelProblems,
  panelValuesFromCriteria,
  withDraftValues,
  type ChoiceListFilter,
  type ModelSearchCriteria,
  type ModelSearchFilter,
  type ModelSearchParameter,
  type PanelProblem,
  type PanelValues,
  type RangeFilter,
  type YesNoFilter,
} from "@/lib/model-search";

import SearchHelp from "./SearchHelp";
import ValueListField from "./ValueListField";
import styles from "./model-search.module.css";

/** Groups open before anything in them is set. */
const GROUPS_OPEN_BY_DEFAULT = new Set(["entry", "model"]);

/**
 * The filter panel of /browse. It starts from the search in the URL and, on
 * Apply, navigates to the URL of the search it describes: the URL is the
 * search, so Back returns to the previous one and a copied link restores it.
 * The page remounts the panel when the URL's search changes.
 *
 * Applying drops the text search (`query`): filters search models, the text
 * search finds entries, and the two do not combine.
 */
export default function FilterPanel({ criteria }: { criteria: ModelSearchCriteria }) {
  const router = useRouter();
  const bodyId = useId();
  const [values, setValues] = useState<PanelValues>(() => panelValuesFromCriteria(criteria));
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [problems, setProblems] = useState<PanelProblem[]>([]);
  // Narrow screens only: the panel folds away above the results.
  const [expanded, setExpanded] = useState(false);
  const [searching, startSearch] = useTransition();
  const activeCount = activeFilterCount(criteria);

  function setParameter(parameter: ModelSearchParameter, list: string[]) {
    setValues((current) => ({ ...current, [parameter]: list }));
    setProblems((current) => current.filter((problem) => problem.parameter !== parameter));
  }

  function problemFor(parameter: ModelSearchParameter): string | null {
    return problems.find((problem) => problem.parameter === parameter)?.message ?? null;
  }

  function apply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const merged = withDraftValues(values, drafts);
    setValues(merged);
    setDrafts({});
    const found = panelProblems(merged);
    setProblems(found);
    if (found.length > 0) {
      return;
    }
    startSearch(() => router.push(browseSearchHref(criteriaFromPanel(merged))));
  }

  function renderFilter(filter: ModelSearchFilter) {
    switch (filter.kind) {
      case "text_list":
        return (
          <ValueListField
            key={filter.id}
            filter={filter}
            values={values[filter.parameter] ?? []}
            draft={drafts[filter.id] ?? ""}
            problem={problemFor(filter.parameter)}
            onValuesChange={(list) => setParameter(filter.parameter, list)}
            onDraftChange={(draft) => setDrafts((current) => ({ ...current, [filter.id]: draft }))}
          />
        );
      case "choice_list":
        return (
          <ChoiceListField
            key={filter.id}
            filter={filter}
            values={values[filter.parameter] ?? []}
            problem={problemFor(filter.parameter)}
            onChange={(list) => setParameter(filter.parameter, list)}
          />
        );
      case "range":
        return (
          <RangeField
            key={filter.id}
            filter={filter}
            values={values}
            problemFor={problemFor}
            onChange={(parameter, value) => setParameter(parameter, value === "" ? [] : [value])}
          />
        );
      case "yes_no":
        return (
          <YesNoField
            key={filter.id}
            filter={filter}
            value={values[filter.parameter]?.[0] ?? ""}
            onChange={(value) => setParameter(filter.parameter, value === "" ? [] : [value])}
          />
        );
      default:
        throw new Error(`unknown model search filter kind: ${(filter as { kind?: unknown }).kind}`);
    }
  }

  return (
    <form
      className={styles.panel}
      role="search"
      aria-label="Filter models"
      onSubmit={apply}
      noValidate
    >
      <div className={styles.panelHead}>
        <h2 className={styles.panelTitle}>Filter models</h2>
        <button
          type="button"
          className={styles.panelToggle}
          aria-expanded={expanded}
          aria-controls={bodyId}
          onClick={() => setExpanded((current) => !current)}
        >
          {activeCount > 0 ? `Filters (${activeCount})` : "Filters"}
        </button>
      </div>
      <div id={bodyId} className={styles.panelBody} data-expanded={expanded ? "true" : undefined}>
        {FILTER_GROUPS.map((group) => {
          const groupCount = activeFilterCount(criteria, group.id);
          return (
            <details
              key={group.id}
              className={styles.group}
              open={GROUPS_OPEN_BY_DEFAULT.has(group.id) || groupCount > 0}
            >
              <summary className={styles.groupSummary}>
                {group.label}
                {groupCount > 0 ? (
                  <span className={styles.groupCount}>
                    {groupCount}
                    <span className={styles.srOnly}> set</span>
                  </span>
                ) : null}
              </summary>
              <div className={styles.groupBody} data-group={group.id}>
                {MODEL_SEARCH_FILTERS.filter((filter) => filter.group === group.id).map(renderFilter)}
              </div>
            </details>
          );
        })}

        {problems.length > 0 ? (
          <p className={styles.panelProblem} role="alert">
            Fix the marked fields to search.
          </p>
        ) : null}
        <div className={styles.panelActions}>
          <button type="submit" className={styles.applyButton} disabled={searching}>
            {searching ? "Searching…" : "Apply filters"}
          </button>
          {activeCount > 0 ? (
            <Link className={styles.clearLink} href="/browse">
              Clear filters
            </Link>
          ) : null}
        </div>
        <SearchHelp />
      </div>
    </form>
  );
}

function ChoiceListField({
  filter,
  values,
  problem,
  onChange,
}: {
  filter: ChoiceListFilter;
  values: string[];
  problem: string | null;
  onChange: (values: string[]) => void;
}) {
  const helpId = useId();
  // A value the API's vocabulary lacks (a hand-edited URL) still shows, so it
  // can be unticked rather than kept invisibly.
  const unknown = values.filter((value) => !filter.choices.some((choice) => choice.value === value));
  const shown = [...filter.choices, ...unknown.map((value) => ({ value, label: value }))];
  return (
    <fieldset className={styles.field} aria-describedby={filter.help ? helpId : undefined}>
      <legend className={styles.fieldLabel}>{filter.label}</legend>
      {filter.help ? (
        <p className={styles.fieldHelp} id={helpId}>
          {filter.help}
        </p>
      ) : null}
      <div className={styles.choices}>
        {shown.map((choice) => {
          const checked = values.includes(choice.value);
          return (
            <label key={choice.value} className={styles.choice}>
              <input
                type="checkbox"
                checked={checked}
                onChange={() =>
                  onChange(
                    checked
                      ? values.filter((value) => value !== choice.value)
                      : [...values, choice.value],
                  )
                }
              />
              <span>{choice.label}</span>
            </label>
          );
        })}
      </div>
      {problem ? (
        <p className={styles.fieldProblem} role="alert">
          {problem}
        </p>
      ) : null}
    </fieldset>
  );
}

function RangeField({
  filter,
  values,
  problemFor,
  onChange,
}: {
  filter: RangeFilter;
  values: PanelValues;
  problemFor: (parameter: ModelSearchParameter) => string | null;
  onChange: (parameter: ModelSearchParameter, value: string) => void;
}) {
  const helpId = useId();
  const label = filter.unit ? `${filter.label} (${filter.unit})` : filter.label;
  const ends = [
    { parameter: filter.minParameter, name: "minimum", placeholder: "min" },
    { parameter: filter.maxParameter, name: "maximum", placeholder: "max" },
  ];
  const messages = [...new Set(ends.map((end) => problemFor(end.parameter)).filter(Boolean))];
  return (
    <fieldset className={styles.range} aria-describedby={filter.help ? helpId : undefined}>
      <legend className={styles.rangeLabel}>{label}</legend>
      <div className={styles.rangeInputs}>
        {ends.map((end, index) => (
          <span key={end.parameter} className={styles.rangeEnd}>
            {index > 0 ? (
              <span className={styles.rangeDash} aria-hidden="true">
                –
              </span>
            ) : null}
            <input
              className={styles.input}
              type="text"
              inputMode="decimal"
              autoComplete="off"
              aria-label={`${filter.label} ${end.name}`}
              placeholder={end.placeholder}
              value={values[end.parameter]?.[0] ?? ""}
              aria-invalid={problemFor(end.parameter) ? true : undefined}
              onChange={(event) => onChange(end.parameter, event.target.value)}
            />
          </span>
        ))}
      </div>
      {filter.help ? (
        <p className={styles.fieldHelp} id={helpId}>
          {filter.help}
        </p>
      ) : null}
      {messages.map((message) => (
        <p key={message} className={styles.fieldProblem} role="alert">
          {message}
        </p>
      ))}
    </fieldset>
  );
}

function YesNoField({
  filter,
  value,
  onChange,
}: {
  filter: YesNoFilter;
  value: string;
  onChange: (value: string) => void;
}) {
  const name = useId();
  const options = [
    { value: "", label: filter.anyLabel },
    { value: "true", label: filter.yesLabel },
    { value: "false", label: filter.noLabel },
  ];
  return (
    <fieldset className={styles.field}>
      <legend className={styles.fieldLabel}>{filter.label}</legend>
      <div className={styles.choices}>
        {options.map((option) => (
          <label key={option.value || "any"} className={styles.choice}>
            <input
              type="radio"
              name={name}
              value={option.value}
              checked={value === option.value}
              onChange={() => onChange(option.value)}
            />
            <span>{option.label}</span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}
