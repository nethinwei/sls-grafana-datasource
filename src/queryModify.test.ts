import fs from 'fs';
import path from 'path';

import {
  addStringFilter,
  hasFieldFilter,
  hasWhereFilter,
  isSQL,
  splitStatement,
  toggleFieldFilter,
  toggleWhereFilter,
  whereCondition,
  addCondition,
  fieldCondition,
} from './queryModify';

describe('splitStatement', () => {
  it.each([
    ['level: ERROR', { search: 'level: ERROR', rest: '' }],
    ['* | select count(*)', { search: '*', rest: '| select count(*)' }],
    ['msg: "a|b" | where x > 1', { search: 'msg: "a|b"', rest: '| where x > 1' }],
  ])('%s', (q, want) => expect(splitStatement(q)).toEqual(want));
});

describe('toggleFieldFilter', () => {
  it.each([
    // [query, key, value, negate, result]
    ['*', 'level', 'ERROR', false, 'level: "ERROR"'],
    ['', 'level', 'ERROR', false, 'level: "ERROR"'],
    ['service: api', 'level', 'ERROR', false, 'service: api and level: "ERROR"'],
    ['a or b', 'level', 'ERROR', false, '(a or b) and level: "ERROR"'],
    ['* | select count(*)', 'level', 'ERROR', false, 'level: "ERROR" | select count(*)'],
    // toggling the same filter removes it
    ['service: api and level: "ERROR"', 'level', 'ERROR', false, 'service: api'],
    ['level: "ERROR"', 'level', 'ERROR', false, '*'],
    ['level: "ERROR" and service: api', 'level', 'ERROR', false, 'service: api'],
    // filter out, and replacing the opposite filter
    ['service: api', 'level', 'INFO', true, 'service: api and not level: "INFO"'],
    ['service: api and level: "INFO"', 'level', 'INFO', true, 'service: api and not level: "INFO"'],
    ['service: api and not level: "INFO"', 'level', 'INFO', false, 'service: api and level: "INFO"'],
    ['service: api and not level: "INFO"', 'level', 'INFO', true, 'service: api'],
    ['*', '__tag__:__path__', '/logs/a "b".log', false, '__tag__:__path__: "/logs/a \\"b\\".log"'],
  ])('%s  %s=%s negate=%s', (q, k, v, negate, want) => {
    expect(toggleFieldFilter(q, k, v, negate)).toBe(want);
  });
});

describe('hasFieldFilter', () => {
  it.each([
    ['level: "ERROR"', true],
    ['service: api and level: "ERROR" | select 1', true],
    ['service: api and not level: "ERROR"', false],
    ['level: ERROR', false],
  ])('%s', (q, want) => expect(hasFieldFilter(q, 'level', 'ERROR')).toBe(want));
});

describe('addStringFilter', () => {
  it('adds and removes free-text filters', () => {
    expect(addStringFilter('level: ERROR', 'timeout', false)).toBe('level: ERROR and "timeout"');
    expect(addStringFilter('level: ERROR and "timeout"', 'timeout', false)).toBe('level: ERROR');
    expect(addStringFilter('*', 'timeout', true)).toBe('not "timeout"');
  });
});

describe('toggleWhereFilter', () => {
  it.each([
    ['level: ERROR', 'http_status', '200', false, `level: ERROR | where "http_status" = '200'`],
    [`level: ERROR | where "http_status" = '200'`, 'http_status', '200', false, 'level: ERROR'],
    [
      `level: ERROR | where "http_status" = '200'`,
      'http_status',
      '200',
      true,
      `level: ERROR | where coalesce("http_status", '') != '200'`,
    ],
    ['*', 'msg', "it's", false, `* | where "msg" = 'it''s'`],
    [`* | where "a" = '1'`, 'a', '1', false, '*'],
  ])('%s  %s=%s negate=%s', (q, k, v, negate, want) => {
    expect(toggleWhereFilter(q, `"${k}"`, v, negate)).toBe(want);
  });

  it('detects its own clauses', () => {
    expect(hasWhereFilter(`* | where "a" = '1'`, '"a"', '1')).toBe(true);
    expect(hasWhereFilter(`* | where "a" != '1'`, '"a"', '1')).toBe(false);
  });
});

describe('review fixes', () => {
  it('treats single quotes as ordinary characters in search', () => {
    expect(splitStatement("msg: it's | select 1")).toEqual({ search: "msg: it's", rest: '| select 1' });
  });

  it('does not take a clause for a longer value as its own', () => {
    const q = toggleWhereFilter('*', '"k"', "a'b", false);
    expect(hasWhereFilter(q, '"k"', 'a')).toBe(false);
    expect(toggleWhereFilter(q, '"k"', 'a', false)).toBe(`* | where "k" = 'a''b' | where "k" = 'a'`);
  });

  it('leaves SQL alone', () => {
    expect(toggleWhereFilter('* | select count(*)', '"k"', 'v', false)).toBe('* | select count(*)');
  });
});

interface SyntaxCases {
  splitStatement: Array<{ in: string; search: string; rest: string }>;
  isSQL: Array<{ in: string; want: boolean }>;
  addCondition: Array<{ query: string; condition: string; want: string }>;
  searchEquals: Array<{ key: string; value: string; negate: boolean; want: string }>;
  whereEquals: Array<{ expr: string; value: string; negate: boolean; want: string }>;
}

/** Cases shared with the backend tests (pkg/syntax_test.go), so both sides write the same syntax. */
describe('shared filter syntax', () => {
  const cases: SyntaxCases = JSON.parse(
    fs.readFileSync(path.join(__dirname, '../testdata/filter-syntax.json'), 'utf8')
  );

  it.each(cases.splitStatement)('splitStatement %j', (c) => {
    expect(splitStatement(c.in)).toEqual({ search: c.search, rest: c.rest });
  });
  it.each(cases.isSQL)('isSQL %j', (c) => {
    expect(isSQL(c.in)).toBe(c.want);
  });
  it.each(cases.addCondition)('addCondition %j', (c) => {
    expect(addCondition(c.query, c.condition)).toBe(c.want);
  });
  it.each(cases.searchEquals)('searchEquals %j', (c) => {
    expect((c.negate ? 'not ' : '') + fieldCondition(c.key, c.value)).toBe(c.want);
  });
  it.each(cases.whereEquals)('whereEquals %j', (c) => {
    expect(whereCondition(c.expr, c.value, c.negate)).toBe(c.want);
  });
});
