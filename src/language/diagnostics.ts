import { splitStatement } from '../queryModify';
import type { CatalogField } from '../types';

/** A hint on a span of the statement (character offsets). */
export interface Diagnostic {
  start: number;
  end: number;
  message: string;
}

/** Replaces the content of quoted strings with spaces, keeping offsets, so they are not scanned. */
function maskQuoted(s: string, quotes: string): string {
  let out = '';
  let quote: string | undefined;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (quote) {
      if (c === '\\' && quote !== "'") {
        out += '  ';
        i++;
        continue;
      }
      if (c === quote) {
        quote = undefined;
        out += c;
      } else {
        out += ' ';
      }
    } else {
      if (quotes.includes(c)) {
        quote = c;
      }
      out += c;
    }
  }
  return out;
}

// `field:` in search; SLS meta fields are written __tag__:__path__.
const SEARCH_FIELD = /(?:^|[\s(])((?:__tag__:)?[A-Za-z_@][\w.\-@]*)\s*:/g;
// Identifiers in SQL: "quoted" or bare.
const SQL_IDENTIFIER = /"((?:[^"]|"")+)"|\b([A-Za-z_][\w.]*)\b/g;

/**
 * Hints from the field catalog that SLS would not give, or only as a confusing error: search on a
 * field without an index (matches nothing), a Grafana-only field name used in SLS, SQL on a field
 * without statistics (null). Only reported when the index is known.
 */
export function fieldDiagnostics(statement: string, fields: CatalogField[], indexKnown: boolean): Diagnostic[] {
  if (!indexKnown || !fields.length) {
    return [];
  }
  const bySLSName = new Map<string, CatalogField>();
  const byName = new Map<string, CatalogField>();
  for (const f of fields) {
    if (f.slsName) {
      bySLSName.set(f.slsName, f);
    }
    byName.set(f.name, f);
  }
  const out: Diagnostic[] = [];
  const { search, rest } = splitStatement(statement);
  const searchStart = statement.indexOf(search);

  const masked = maskQuoted(search, '"\'');
  for (const m of masked.matchAll(SEARCH_FIELD)) {
    const name = m[1];
    const start = searchStart + m.index! + m[0].indexOf(name);
    const span = { start, end: start + name.length };
    const sls = bySLSName.get(name);
    if (sls) {
      if (sls.indexed === false) {
        out.push({
          ...span,
          message: `${name} has no index, so "${name}: value" matches nothing. Add a field index in SLS, or filter with | where "${name}" = 'value'.`,
        });
      }
      continue;
    }
    const shown = byName.get(name);
    if (shown?.slsName) {
      out.push({
        ...span,
        message: `${name} is the Grafana name of SLS field ${shown.slsName}; use ${shown.slsName} in queries.`,
      });
    } else if (shown) {
      out.push({
        ...span,
        message: `${name} is produced by a ${shown.rule ?? 'field'} rule and exists only in Grafana.`,
      });
    }
  }

  if (/\|\s*select\b/i.test(rest)) {
    const restStart = statement.length - rest.length;
    const sql = maskQuoted(rest, "'");
    for (const m of sql.matchAll(SQL_IDENTIFIER)) {
      const name = m[1] !== undefined ? m[1].replace(/""/g, '"') : m[2];
      const f = bySLSName.get(name);
      if (f && f.stats === false) {
        const start = restStart + m.index!;
        out.push({
          start,
          end: start + m[0].length,
          message: `${name} has no SQL statistics, so SQL sees it as null. Enable statistics for it in the field index.`,
        });
      }
    }
  }
  return out;
}
