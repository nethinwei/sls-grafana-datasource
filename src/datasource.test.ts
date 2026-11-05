import { CoreApp, type DataSourceInstanceSettings } from '@grafana/data';
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
