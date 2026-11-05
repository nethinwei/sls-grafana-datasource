import type { monacoTypes } from '@grafana/ui';

import { splitStatement } from '../queryModify';
import type { CatalogField } from '../types';
import { functions, keywords, operators } from './slsterms';

interface Suggestion {
  label: string;
  insertText: string;
  kind: monacoTypes.languages.CompletionItemKind;
  detail?: string;
  snippet?: boolean;
  filterText?: string;
  sortText?: string;
}

// Values of monaco.languages.CompletionItemKind; the enum itself is only available at runtime.
const KIND = { Function: 1, Field: 3, Variable: 4, Keyword: 17, Operator: 11, Snippet: 27 } as const;

export const MACROS: Suggestion[] = [
  {
    label: '$__timeGroup',
    insertText: '\\$__timeGroup(${1:__time__})',
    kind: KIND.Snippet,
    detail: 'Time bucket of the panel interval: (col - col % interval). Optional 2nd arg: $__timeGroup(__time__, 5m)',
    snippet: true,
  },
  {
    label: '$__interval_s',
    insertText: '\\$__interval_s',
    kind: KIND.Variable,
    detail: 'Panel interval in seconds',
    snippet: true,
  },
  {
    label: '$__timeFrom',
    insertText: '\\$__timeFrom',
    kind: KIND.Variable,
    detail: 'Range start, unix seconds',
    snippet: true,
  },
  {
    label: '$__timeTo',
    insertText: '\\$__timeTo',
    kind: KIND.Variable,
    detail: 'Range end, unix seconds',
    snippet: true,
  },
];

const STATIC: Suggestion[] = [
  ...MACROS,
  ...keywords.map((k) => ({ label: k.label, insertText: k.label, kind: KIND.Keyword })),
  ...operators.map((o) => ({ label: o.label, insertText: o.label, kind: KIND.Operator })),
  ...Object.keys(functions).map((name) => ({
    label: name,
    insertText: `${name}(\${1})`,
    kind: KIND.Function,
    snippet: true,
  })),
];

/** Catalog fields per editor model (keyed by model URI), set by each editor for its logstore. */
const fieldsByModel = new Map<string, CatalogField[]>();

export function setModelFields(modelUri: string, fields: CatalogField[] | undefined) {
  if (fields?.length) {
    fieldsByModel.set(modelUri, fields);
  } else {
    fieldsByModel.delete(modelUri);
  }
}

export function getModelFields(modelUri: string): CatalogField[] | undefined {
  return fieldsByModel.get(modelUri);
}

/** Index state of a field in a few words, e.g. "text · search · SQL". */
export function fieldSummary(f: CatalogField): string {
  const parts = [f.type || 'field'];
  if (f.indexed === false) {
    parts.push('no index: use | where');
  } else if (f.indexed) {
    parts.push(f.stats ? 'search · SQL' : f.stats === false ? 'search only' : 'search');
  }
  return parts.join(' · ');
}

const PLAIN_IDENTIFIER = /^[A-Za-z_][A-Za-z0-9_]*$/;

/**
 * A catalog field as a suggestion: shown under its Grafana name, inserting its SLS name (quoted in
 * SQL / SPL when it is not a plain identifier). Fields produced by rules exist only in Grafana and
 * are not suggested.
 */
function fieldSuggestion(f: CatalogField, inPipe: boolean): Suggestion | undefined {
  if (!f.slsName) {
    return undefined;
  }
  const insert = inPipe && !PLAIN_IDENTIFIER.test(f.slsName) ? `"${f.slsName.replace(/"/g, '""')}"` : f.slsName;
  return {
    label: f.name,
    insertText: insert,
    kind: KIND.Field,
    detail: f.slsName === f.name ? fieldSummary(f) : `${fieldSummary(f)} · SLS: ${f.slsName}`,
    filterText: f.slsName === f.name ? f.name : `${f.name} ${f.slsName}`,
    // Searchable fields first.
    sortText: (f.indexed === false ? '1' : '0') + f.name,
  };
}

export function getCompletionProvider(monaco: typeof monacoTypes): monacoTypes.languages.CompletionItemProvider {
  return {
    triggerCharacters: ['$'],
    provideCompletionItems(model, position) {
      const word = model.getWordUntilPosition(position);
      // Replace the whole token typed so far: field names contain ":" and "." (__tag__:__path__,
      // host.name) and macros start with "$".
      const before = model.getLineContent(position.lineNumber).slice(0, position.column - 1);
      const token = /[\w.:@$-]*$/.exec(before)?.[0] ?? '';
      const startColumn = position.column - token.length;
      const range = new monaco.Range(position.lineNumber, startColumn, position.lineNumber, word.endColumn);
      const textBefore = model.getValueInRange(new monaco.Range(1, 1, position.lineNumber, position.column));
      const inPipe = splitStatement(textBefore).rest !== '';
      const fields = (fieldsByModel.get(model.uri.toString()) ?? [])
        .map((f) => fieldSuggestion(f, inPipe))
        .filter((s): s is Suggestion => s !== undefined);
      return {
        suggestions: [...fields, ...STATIC].map((s) => ({
          label: s.label,
          kind: s.kind,
          detail: s.detail,
          filterText: s.filterText,
          sortText: s.sortText,
          insertText: s.insertText,
          insertTextRules: s.snippet ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet : undefined,
          range,
        })),
      };
    },
  };
}
