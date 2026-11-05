/**
 * Helpers that edit the search part of an SLS statement: the text before the first `|` outside
 * quotes. The SQL or SPL after it is kept as is, so filters also apply to SQL panels. Dashboard ad hoc
 * filters are translated by the backend (pkg/filters.go), which knows the field catalog.
 */

/**
 * Splits `search | rest` at the first pipe that is not inside double quotes. A single quote is an
 * ordinary character in SLS search (msg: it's).
 */
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
    } else if (c === '"') {
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

/** A field name for SLS search, quoted when it has characters other than [\w.@:-] (spaces, ...). */
function searchKey(key: string): string {
  return /^[\w.@:-]+$/.test(key) ? key : quoteValue(key);
}

/** `key: "value"` */
export function fieldCondition(key: string, value: string): string {
  return `${searchKey(key)}: ${quoteValue(value)}`;
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
    } else if (c === '"') {
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
 * Where condition on an SPL expression: a quoted field ("http_status") or, for flattened JSON
 * fields, json_extract_scalar(...). A missing field is null in SPL; ➖ compares it as "" so that,
 * as in Loki, logs without the field remain.
 */
export function whereCondition(expr: string, value: string, negate: boolean): string {
  return negate ? `coalesce(${expr}, '') != ${splString(value)}` : `${expr} = ${splString(value)}`;
}

function whereClause(expr: string, value: string, negate: boolean): string {
  return '| where ' + whereCondition(expr, value, negate);
}

/** Whether the statement has `| where <expr> = 'value'` added by toggleWhereFilter. */
export function hasWhereFilter(query: string, expr: string, value: string): boolean {
  return indexOfClause(query, whereClause(expr, value, false)) >= 0;
}

/** Position of a where clause, not counting a clause for a longer value ('a' inside 'a''b'). */
function indexOfClause(query: string, clause: string): number {
  for (let i = query.indexOf(clause); i >= 0; i = query.indexOf(clause, i + 1)) {
    if (query[i + clause.length] !== "'") {
      return i;
    }
  }
  return -1;
}

/** Whether the statement has SQL, where SPL clauses cannot be added. */
export function isSQL(query: string): boolean {
  return /^\|\s*(select\b|set\s+session\b)/i.test(splitStatement(query).rest);
}

/**
 * ➕ / ➖ on a field that SLS search cannot use (no index, flattened JSON): SPL can filter it.
 * Appends `| where <expr> = 'value'` (or `!=`); clicking the same one again removes it, and the
 * opposite clause on the same value is replaced.
 */
export function toggleWhereFilter(query: string, expr: string, value: string, negate: boolean): string {
  const wanted = whereClause(expr, value, negate);
  const opposite = whereClause(expr, value, !negate);
  if (isSQL(query)) {
    return query; // SPL cannot follow SQL
  }
  const strip = (q: string, clause: string) => {
    const i = indexOfClause(q, clause);
    return i < 0 ? q : (q.slice(0, i) + q.slice(i + clause.length)).replace(/\s{2,}/g, ' ').trim();
  };
  if (indexOfClause(query, wanted) >= 0) {
    return strip(query, wanted) || '*';
  }
  const base = strip(query, opposite) || '*';
  return `${base} ${wanted}`;
}
