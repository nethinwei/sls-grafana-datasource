import {
  addStringFilter,
  applyAdHocFilters,
  regexToWildcard,
  hasFieldFilter,
  splitStatement,
  toggleFieldFilter,
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

describe('applyAdHocFilters', () => {
  it('turns dashboard filters into search conditions', () => {
    const q = applyAdHocFilters('* | select count(*) as c', [
      { key: 'service', operator: '=', value: 'api' },
      { key: 'level', operator: '!=', value: 'INFO' },
      { key: 'channel', operator: '=|', value: 'a', values: ['a', 'b'] },
      { key: 'cost_ms', operator: '>', value: '100' },
      { key: 'msg', operator: '=~', value: '^time.*' },
      { key: 'path', operator: '!~', value: '.*health' }, // leading wildcard: not expressible, skipped
    ]);
    expect(q).toBe(
      'service: "api" and not level: "INFO" and (channel: "a" or channel: "b") and cost_ms > 100 and msg: time* | select count(*) as c'
    );
    expect(applyAdHocFilters('level: ERROR', [])).toBe('level: ERROR');
  });
});

describe('regexToWildcard', () => {
  it.each([
    ['^pay.*', 'pay*'],
    ['timeout$', 'timeout'],
    ['a.c', 'a?c'],
    ['/api/v1/.*', '/api/v1/*'],
    ['.*health', undefined],
    ['(a|b)', undefined],
    ['has space.*', undefined],
  ])('%s', (re, want) => expect(regexToWildcard(re)).toBe(want));
});
