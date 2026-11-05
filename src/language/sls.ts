import type { monacoTypes } from '@grafana/ui';
import { functions, keywords, operators } from './slsterms';
import _ from 'lodash';

export const languageConfiguration: monacoTypes.languages.LanguageConfiguration = {
  wordPattern: /(-?\d*\.\d\w*)|([^`~!#%^&*()\-=+\[{\]}\\|;:'",.<>\/?\s]+)/g,
  comments: {
    lineComment: '#',
  },
  brackets: [
    ['{', '}'],
    ['[', ']'],
    ['(', ')'],
  ],
  autoClosingPairs: [
    { open: '{', close: '}' },
    { open: '[', close: ']' },
    { open: '(', close: ')' },
    { open: '"', close: '"' },
    { open: "'", close: "'" },
  ],
  surroundingPairs: [
    { open: '{', close: '}' },
    { open: '[', close: ']' },
    { open: '(', close: ')' },
    { open: '"', close: '"' },
    { open: "'", close: "'" },
    { open: '<', close: '>' },
  ],
  folding: {},
};

// const sls_functions = functions

const slsFunctionsList = Object.keys(functions);
const slsFuncitonsSuggest = slsFunctionsList.map((value) => {
  return {
    label: value,
    type: _.get(functions, value).type,
  };
});

const slsKeywordsList = keywords.map((value) => {
  return value.label;
});
const slsoperatorList = operators.map((value) => {
  return value.label;
});
const allSuggestions = slsFuncitonsSuggest.concat(keywords).concat(operators);

export const language = {
  ignoreCase: true,
  defaultToken: '',
  tokenPostfix: '.sls',
  vectorMatching: '',

  symbols: /[=><!~?:&|+\-*\/^%]+/,
  escapes: /\\(?:[abfnrtv\\"']|x[0-9A-Fa-f]{1,4}|u[0-9A-Fa-f]{4}|U[0-9A-Fa-f]{8})/,
  digits: /\d+(_+\d+)*/,
  octaldigits: /[0-7]+(_+[0-7]+)*/,
  binarydigits: /[0-1]+(_+[0-1]+)*/,
  hexdigits: /[[0-9a-fA-F]+(_+[0-9a-fA-F]+)*/,
  integersuffix: /(ll|LL|u|U|l|L)?(ll|LL|u|U|l|L)?/,
  floatsuffix: /[fFlL]?/,

  keywords: slsKeywordsList,
  operators: slsoperatorList,
  builtinFunctions: slsFunctionsList,
  builtinVariables: [],
  pseudoColumns: ['$ACTION', '$IDENTITY', '$ROWGUID', '$PARTITION'],
  tokenizer: {
    root: [
      { include: '@comments' },
      { include: '@whitespace' },
      { include: '@pseudoColumns' },
      { include: '@numbers' },
      { include: '@strings' },
      { include: '@complexIdentifiers' },
      { include: '@scopes' },
      [/[;,.]/, 'delimiter'],
      [/[()]/, '@brackets'],
      [
        /[\w@#$]+/,
        {
          cases: {
            '@operators': 'operator',
            '@builtinVariables': 'predefined',
            '@builtinFunctions': 'predefined',
            '@keywords': 'keyword',
            '@default': 'identifier',
          },
        },
      ],
      [/[<>=!%&+\-*/|~^]/, 'operator'],
    ],
    whitespace: [[/\s+/, 'white']],
    comments: [
      [/--+.*/, 'comment'],
      [/\/\*/, { token: 'comment.quote', next: '@comment' }],
    ],
    comment: [
      [/[^*/]+/, 'comment'],
      [/\*\//, { token: 'comment.quote', next: '@pop' }],
      [/./, 'comment'],
    ],
    pseudoColumns: [
      [
        /[$][A-Za-z_][\w@#$]*/,
        {
          cases: {
            '@pseudoColumns': 'predefined',
            '@default': 'identifier',
          },
        },
      ],
    ],
    numbers: [
      [/0[xX][0-9a-fA-F]*/, 'number'],
      [/[$][+-]*\d*(\.\d*)?/, 'number'],
      [/((\d+(\.\d*)?)|(\.\d+))([eE][\-+]?\d+)?/, 'number'],
    ],
    strings: [
      [/N'/, { token: 'string', next: '@string' }],
      [/'/, { token: 'string', next: '@string' }],
    ],
    string: [
      [/[^']+/, 'string'],
      [/''/, 'string'],
      [/'/, { token: 'string', next: '@pop' }],
    ],
    complexIdentifiers: [
      [/\[/, { token: 'identifier.quote', next: '@bracketedIdentifier' }],
      [/"/, { token: 'identifier.quote', next: '@quotedIdentifier' }],
    ],
    bracketedIdentifier: [
      [/[^\]]+/, 'identifier'],
      [/]]/, 'identifier'],
      [/]/, { token: 'identifier.quote', next: '@pop' }],
    ],
    quotedIdentifier: [
      [/[^"]+/, 'identifier'],
      [/""/, 'identifier'],
      [/"/, { token: 'identifier.quote', next: '@pop' }],
    ],
    scopes: [
      [/BEGIN\s+(DISTRIBUTED\s+)?TRAN(SACTION)?\b/i, 'keyword'],
      [/BEGIN\s+TRY\b/i, { token: 'keyword.try' }],
      [/END\s+TRY\b/i, { token: 'keyword.try' }],
      [/BEGIN\s+CATCH\b/i, { token: 'keyword.catch' }],
      [/END\s+CATCH\b/i, { token: 'keyword.catch' }],
      [/(BEGIN|CASE)\b/i, { token: 'keyword.block' }],
      [/END\b/i, { token: 'keyword.block' }],
      [/WHEN\b/i, { token: 'keyword.choice' }],
      [/THEN\b/i, { token: 'keyword.choice' }],
    ],
  },
} as monacoTypes.languages.IMonarchLanguage;

export const completionItemProvider: monacoTypes.languages.CompletionItemProvider = {
  provideCompletionItems: () => {
    const suggestions = allSuggestions.map((value) => {
      return {
        label: value,
        kind: value.type,
        insertText: value.label,
        detail: value.label,
        insertTextRules: 4 as monacoTypes.languages.CompletionItemInsertTextRule.InsertAsSnippet,
      } as unknown as monacoTypes.languages.CompletionItem;
    });
    return { suggestions } as monacoTypes.languages.ProviderResult<monacoTypes.languages.CompletionList>;
  },
};
