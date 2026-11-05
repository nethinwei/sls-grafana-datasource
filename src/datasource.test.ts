import { CoreApp, type DataSourceInstanceSettings, SupplementaryQueryType } from '@grafana/data';
import type { TemplateSrv } from '@grafana/runtime';

import { DEFAULT_SQL, SLSDataSource, toVariableQuery } from './datasource';
import type { SLSDataSourceOptions } from './types';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: () => ({ getVariables: () => [], replace: (s: string) => s }),
}));

const settings = {
  id: 1,
  uid: 'sls',
  type: 'aliyun-log-service-datasource',
  name: 'SLS',
  jsonData: { project: 'p', logstore: 'default-ls' },
  meta: {} as never,
  readOnly: false,
  access: 'proxy',
} as unknown as DataSourceInstanceSettings<SLSDataSourceOptions>;

const passthrough = { getVariables: () => [], replace: (s?: string) => s ?? '' } as unknown as TemplateSrv;

describe('SLSDataSource', () => {
  const ds = new SLSDataSource(settings, passthrough);

  it('starts Explore with a log search and panels with an event count', () => {
    expect(ds.getDefaultQuery(CoreApp.Explore)).toEqual({ query: '*' });
    expect(ds.getDefaultQuery(CoreApp.Dashboard)).toEqual({ query: DEFAULT_SQL });
    expect(ds.getDefaultQuery(CoreApp.UnifiedAlerting)).toEqual({ query: DEFAULT_SQL });
  });

  it('skips hidden queries', () => {
    expect(ds.filterQuery({ refId: 'A', hide: true })).toBe(false);
    expect(ds.filterQuery({ refId: 'A' })).toBe(true);
  });

  it('shows the logstore in query history', () => {
    expect(ds.getQueryDisplayText({ refId: 'A', query: 'level: ERROR' })).toBe('default-ls: level: ERROR');
    expect(ds.getQueryDisplayText({ refId: 'A', logstore: 'other', query: '*' })).toBe('other: *');
  });
});

describe('toVariableQuery', () => {
  it.each([
    ['* | select a from log', { refId: 'variable', query: '* | select a from log' }],
    [{ __legacyStringValue: '* | select b from log' }, { refId: 'variable', query: '* | select b from log' }],
    [
      { refId: 'X', query: 'q', logstore: 'ls' },
      { refId: 'X', query: 'q', logstore: 'ls' },
    ],
    [undefined, { refId: 'variable' }],
  ])('%j', (stored, want) => {
    expect(toVariableQuery(stored as never)).toEqual(want);
  });
});

describe('log features', () => {
  const ds = new SLSDataSource(
    { ...settings, jsonData: { ...settings.jsonData, levelField: 'severity' } } as typeof settings,
    passthrough
  );

  it('filters on fields from Explore, mapping detected_level to the level field', () => {
    const q = { refId: 'A', query: 'service: api' };
    expect(
      ds.toggleQueryFilter(q, { type: 'FILTER_FOR', options: { key: 'detected_level', value: 'ERROR' } }).query
    ).toBe('service: api and severity: "ERROR"');
    expect(ds.toggleQueryFilter(q, { type: 'FILTER_OUT', options: { key: 'channel', value: 'x' } }).query).toBe(
      'service: api and not channel: "x"'
    );
    // Fields added by the plugin are not SLS fields.
    expect(ds.toggleQueryFilter(q, { type: 'FILTER_FOR', options: { key: 'message', value: 'x' } })).toBe(q);
    expect(
      ds.queryHasFilter({ refId: 'A', query: 'severity: "ERROR"' }, { key: 'detected_level', value: 'ERROR' })
    ).toBe(true);
    expect(ds.modifyQuery(q, { type: 'ADD_STRING_FILTER_OUT', options: { value: 'timeout' } }).query).toBe(
      'service: api and not "timeout"'
    );
  });

  it('asks for a logs volume only for plain searches', () => {
    const volume = { type: SupplementaryQueryType.LogsVolume } as const;
    expect(ds.getSupplementaryQuery(volume, { refId: 'A', query: 'level: ERROR' })).toEqual({
      refId: 'log-volume-A',
      query: 'level: ERROR',
      queryType: 'logsVolume',
    });
    expect(ds.getSupplementaryQuery(volume, { refId: 'A', query: '* | select count(*)' })).toBeUndefined();
    expect(ds.getSupplementaryQuery(volume, { refId: 'A', query: '* | where a > 1' })).toBeUndefined();
  });

  it('applies dashboard ad hoc filters to the search part', () => {
    const q = ds.applyTemplateVariables({ refId: 'A', query: '* | select count(*) as c' }, {}, [
      { key: 'service', operator: '=', value: 'api' },
    ]);
    expect(q.query).toBe('service: "api" | select count(*) as c');
  });
});
