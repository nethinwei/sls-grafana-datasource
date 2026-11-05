/**
 * Helpers that edit the search part of an SLS statement: the text before the first `|` outside
 * quotes. The SQL or SPL after it is kept as is, so filters also apply to SQL panels. Dashboard ad hoc
 * filters are translated by the backend (pkg/filters.go), which knows the field catalog.
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

// ---------- SPL `| where` filters, for fields without an SLS index ----------

function splString(value: string): string {
  return "'" + value.replace(/'/g, "''") + "'";
}

/**
 * `expr` replaces the quoted field, e.g. json_extract_scalar("http", '$["method"]'). A missing
 * field is null in SPL; ➖ compares it as "" so that, as in Loki, logs without the field remain.
 */
function whereClause(key: string, value: string, negate: boolean, expr?: string): string {
  const col = expr ?? `"${key.replace(/"/g, '""')}"`;
  return negate ? `| where coalesce(${col}, '') != ${splString(value)}` : `| where ${col} = ${splString(value)}`;
}

/** Whether the statement has `| where "key" = 'value'` added by toggleWhereFilter. */
export function hasWhereFilter(query: string, key: string, value: string, expr?: string): boolean {
  return query.includes(whereClause(key, value, false, expr));
}

/**
 * ➕ / ➖ on a field that has no index: SLS search cannot use it, but SPL can filter any field.
 * Appends `| where "key" = 'value'` (or `!=`); clicking the same one again removes it, and the
 * opposite clause on the same value is replaced.
 */
export function toggleWhereFilter(query: string, key: string, value: string, negate: boolean, expr?: string): string {
  const wanted = whereClause(key, value, negate, expr);
  const opposite = whereClause(key, value, !negate, expr);
  const strip = (q: string, clause: string) =>
    q
      .replace(' ' + clause, '')
      .replace(clause, '')
      .trim();
  if (query.includes(wanted)) {
    return strip(query, wanted) || '*';
  }
  const base = strip(query, opposite) || '*';
  return `${base} ${wanted}`;
}
