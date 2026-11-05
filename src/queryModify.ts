import type { AdHocVariableFilter } from '@grafana/data';

/**
 * Helpers that edit the search part of an SLS statement: the text before the first `|` outside
 * quotes. The SQL or SPL after it is kept as is, so filters also apply to SQL panels.
 */

/** Splits `search | rest` at the first pipe that is not inside quotes. */
export function splitStatement(query: string): { search: string; rest: string } {
  let quote: string | undefined;
  for (let i = 0; i < query.length; i++) {
    const c = query[i];
    if (quote) {
      if (c === '\\') {
        i++;
      } else if (c === quote) {
        quote = undefined;
      }
    } else if (c === '"' || c === "'") {
      quote = c;
    } else if (c === '|') {
      return { search: query.slice(0, i).trim(), rest: query.slice(i) };
    }
  }
  return { search: query.trim(), rest: '' };
}

function join(search: string, rest: string): string {
  return rest ? `${search} ${rest}` : search;
}

function quoteValue(value: string): string {
  return '"' + value.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"';
}

/** `key: "value"` */
export function fieldCondition(key: string, value: string): string {
  return `${key}: ${quoteValue(value)}`;
}

function isMatchAll(search: string): boolean {
  return search === '' || search === '*';
}

/** Whether `or` appears outside parentheses and quotes, so appending `and ...` would change precedence. */
function hasTopLevelOr(search: string): boolean {
  let depth = 0;
  let quote: string | undefined;
  for (let i = 0; i < search.length; i++) {
    const c = search[i];
    if (quote) {
      if (c === '\\') {
        i++;
      } else if (c === quote) {
        quote = undefined;
      }
    } else if (c === '"' || c === "'") {
      quote = c;
    } else if (c === '(') {
      depth++;
    } else if (c === ')') {
      depth--;
    } else if (depth === 0 && /^\sor\s/i.test(search.slice(i, i + 4))) {
      return true;
    }
  }
  return false;
}

/** Appends a condition to the search part with `and`. */
export function addCondition(query: string, condition: string): string {
  const { search, rest } = splitStatement(query);
  if (isMatchAll(search)) {
    return join(condition, rest);
  }
  const base = hasTopLevelOr(search) ? `(${search})` : search;
  return join(`${base} and ${condition}`, rest);
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** Removes a condition added by addCondition (with its `and`); returns undefined when absent. */
function removeCondition(query: string, condition: string): string | undefined {
  const { search, rest } = splitStatement(query);
  const c = escapeRegExp(condition);
  const patterns = [
    new RegExp(`\\s+and\\s+${c}(?=\\s|$)`, 'i'),
    new RegExp(`^${c}\\s+and\\s+`, 'i'),
    new RegExp(`^${c}$`, 'i'),
  ];
  for (const p of patterns) {
    if (p.test(search)) {
      const out = search.replace(p, '').trim();
      return join(out === '' ? '*' : out, rest);
    }
  }
  return undefined;
}

/** Whether the search part contains `key: "value"` (not negated). */
export function hasFieldFilter(query: string, key: string, value: string): boolean {
  const { search } = splitStatement(query);
  const c = escapeRegExp(fieldCondition(key, value));
  return new RegExp(`(^|[\\s(])${c}(?=[\\s)]|$)`, 'i').test(search) && !new RegExp(`not\\s+${c}`, 'i').test(search);
}

/**
 * Explore's ➕ / ➖ on a field: adds `key: "value"` or `not key: "value"`; clicking the same one
 * again removes it, and the opposite filter on the same value is replaced.
 */
export function toggleFieldFilter(query: string, key: string, value: string, negate: boolean): string {
  const positive = fieldCondition(key, value);
  const negative = `not ${positive}`;
  const wanted = negate ? negative : positive;
  const opposite = negate ? positive : negative;

  const removed = removeCondition(query, wanted);
  if (removed !== undefined) {
    return removed;
  }
  const base = removeCondition(query, opposite) ?? query;
  return addCondition(base, wanted);
}

/** Free-text filter: `"text"` or `not "text"`. */
export function addStringFilter(query: string, value: string, negate: boolean): string {
  const condition = (negate ? 'not ' : '') + quoteValue(value);
  return removeCondition(query, condition) ?? addCondition(query, condition);
}

/**
 * SLS search has no regex but supports `*` / `?` wildcards (not at the start, value unquoted).
 * Converts simple regexes such as `^pay.*`, `timeout$` or `a.c`; returns undefined otherwise.
 */
export function regexToWildcard(regex: string): string | undefined {
  const body = regex.replace(/^\^/, '').replace(/\$$/, '');
  const wildcard = body.replace(/\.\*/g, '*').replace(/\./g, '?');
  if (!/^[\w\-/:@*?]+$/.test(wildcard) || /^[*?]/.test(wildcard)) {
    return undefined;
  }
  return wildcard;
}

/**
 * Ad hoc filters of dashboards as an SLS search condition. Supported operators: =, !=, =| / !=|
 * (one of several values), =~ / !~ (simple regexes, as wildcards), <, >, <=, >= (numeric fields).
 * Filters that SLS search cannot express are skipped.
 */
export function adHocCondition(filters: AdHocVariableFilter[]): string {
  const parts: string[] = [];
  for (const f of filters) {
    const values = f.values?.length ? f.values : [f.value];
    switch (f.operator) {
      case '=':
        parts.push(fieldCondition(f.key, f.value));
        break;
      case '!=':
        parts.push(`not ${fieldCondition(f.key, f.value)}`);
        break;
      case '=|':
        parts.push(
          values.length === 1
            ? fieldCondition(f.key, values[0])
            : `(${values.map((v) => fieldCondition(f.key, v)).join(' or ')})`
        );
        break;
      case '!=|':
        parts.push(...values.map((v) => `not ${fieldCondition(f.key, v)}`));
        break;
      case '=~':
      case '!~': {
        const wildcard = regexToWildcard(f.value);
        if (wildcard) {
          parts.push(`${f.operator === '!~' ? 'not ' : ''}${f.key}: ${wildcard}`);
        }
        break;
      }
      case '<':
      case '>':
      case '<=':
      case '>=':
        if (!Number.isNaN(Number(f.value))) {
          parts.push(`${f.key} ${f.operator} ${Number(f.value)}`);
        }
        break;
    }
  }
  return parts.join(' and ');
}

export function applyAdHocFilters(query: string, filters: AdHocVariableFilter[] | undefined): string {
  const condition = filters?.length ? adHocCondition(filters) : '';
  return condition ? addCondition(query, condition) : query;
}
