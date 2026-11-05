import type { monacoTypes } from '@grafana/ui';

import { functions, keywords, operators } from './slsterms';

interface Suggestion {
  label: string;
  insertText: string;
  kind: monacoTypes.languages.CompletionItemKind;
  detail?: string;
  snippet?: boolean;
}

// Values of monaco.languages.CompletionItemKind; the enum itself is only available at runtime.
const KIND = { Function: 1, Variable: 4, Keyword: 17, Operator: 11, Snippet: 27 } as const;

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

export function getCompletionProvider(monaco: typeof monacoTypes): monacoTypes.languages.CompletionItemProvider {
  return {
    triggerCharacters: ['$'],
    provideCompletionItems(model, position) {
      const word = model.getWordUntilPosition(position);
      // Include a leading "$" so macros replace what was typed.
      const before = model.getLineContent(position.lineNumber).slice(0, word.startColumn - 1);
      const startColumn = before.endsWith('$') ? word.startColumn - 1 : word.startColumn;
      const range = new monaco.Range(position.lineNumber, startColumn, position.lineNumber, word.endColumn);
      return {
        suggestions: STATIC.map((s) => ({
          label: s.label,
          kind: s.kind,
          detail: s.detail,
          insertText: s.insertText,
          insertTextRules: s.snippet ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet : undefined,
          range,
        })),
      };
    },
  };
}
