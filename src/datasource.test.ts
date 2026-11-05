import { CoreApp, type DataSourceInstanceSettings, SupplementaryQueryType } from '@grafana/data';
import type { TemplateSrv } from '@grafana/runtime';

import { DEFAULT_SQL, SLSDataSource, toVariableQuery } from './datasource';
import type { Catalog, SLSDataSourceOptions } from './types';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: () => ({ getVariables: () => [], replace: (s: string) => s }),
}));

const settings = {
  id: 1,
  uid: 'sls',
  type: 'nethinwei-sls-datasource',
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
    [
      { refId: 'X', query: 'q', logstore: 'ls' },
      { refId: 'X', query: 'q', logstore: 'ls', format: 'table' },
    ],
    [undefined, { refId: 'variable', format: 'table' }],
  ])('%j', (stored, want) => {
    expect(toVariableQuery(stored)).toEqual(want);
  });
});

describe('log features', () => {
  const ds = new SLSDataSource(
    { ...settings, jsonData: { ...settings.jsonData, logs: { levelField: 'severity' } } } as typeof settings,
    passthrough
  );

  it('filters on fields from Explore, mapping detected_level to the level field', () => {
    const q = { refId: 'A', query: 'service: api' };
    // Before any result describes the fields: tried as indexed SLS fields.
    expect(
      ds.toggleQueryFilter(q, { type: 'FILTER_FOR', options: { key: 'detected_level', value: 'ERROR' } }).query
    ).toBe('service: api and severity: "ERROR"');
    expect(ds.toggleQueryFilter(q, { type: 'FILTER_OUT', options: { key: 'channel', value: 'x' } }).query).toBe(
      'service: api and not channel: "x"'
    );
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

  it('sends dashboard ad hoc filters to the backend', () => {
    const filters = [{ key: 'service', operator: '=', value: 'api' }];
    const q = ds.applyTemplateVariables({ refId: 'A', query: '* | select count(*) as c' }, {}, filters);
    expect(q.query).toBe('* | select count(*) as c');
    expect(q.adhocFilters).toEqual(filters);
    // Filters carried over in the query (e.g. into Explore) do not survive a request without them.
    expect(ds.applyTemplateVariables({ ...q }, {}).adhocFilters).toBeUndefined();
  });
});

describe('field catalog', () => {
  const catalog: Catalog = {
    indexKnown: true,
    fields: [
      { name: 'level', type: 'text', indexed: true, stats: true, slsName: 'level' },
      { name: 'http_status', indexed: false, stats: false, slsName: 'http_status' },
      { name: 'host.name', type: 'text', indexed: true, stats: true, slsName: '__tag__:__hostname__', rule: 'rename' },
      { name: 'service.name', rule: 'extract' },
      { name: '__tag__:__path__', indexed: true, slsName: '__tag__:__path__' },
    ],
  };
  async function loaded() {
    const ds = new SLSDataSource(settings, passthrough);
    const post = jest.fn().mockResolvedValue(catalog);
    ds.postResource = post as never;
    await ds.getCatalog();
    return { ds, post };
  }

  it('offers filterable fields as ad hoc keys and caches the catalog', async () => {
    const { ds, post } = await loaded();
    expect((await ds.getTagKeys()).map((k) => k.text)).toEqual(['level', 'http_status', 'host.name']);
    expect(post).toHaveBeenCalledTimes(1);
    await ds.getCatalog(undefined, true);
    expect(post).toHaveBeenCalledTimes(2);
  });
});

describe('filters described by the backend', () => {
  // As the backend describes the fields of a logs frame (describeFilters in pkg/datasource.go).
  const frame = {
    fields: [],
    length: 0,
    meta: {
      custom: {
        logstore: 'default-ls',
        filterFields: {
          message: null,
          'service.name': null,
          'host.name': { key: '__tag__:__hostname__', expr: '"__tag__:__hostname__"', search: true },
          http_status: { key: 'http_status', expr: '"http_status"', search: false },
          http_method: { expr: `coalesce("http_method", json_extract_scalar("http", '$["method"]'))`, search: false },
        },
      },
    },
  };
  const q = { refId: 'A', query: 'service: api' };

  it('writes the conditions the backend describes', () => {
    const ds = new SLSDataSource(settings, passthrough);
    const toggle = (key: string, value: string, type: 'FILTER_FOR' | 'FILTER_OUT' = 'FILTER_FOR') =>
      ds.toggleQueryFilter(q, { type, options: { key, value }, frame }).query;
    expect(toggle('host.name', 'web-1')).toBe('service: api and __tag__:__hostname__: "web-1"');
    expect(toggle('http_status', '200')).toBe(`service: api | where "http_status" = '200'`);
    expect(toggle('http_method', 'GET', 'FILTER_OUT')).toBe(
      `service: api | where coalesce(coalesce("http_method", json_extract_scalar("http", '$["method"]')), '') != 'GET'`
    );
    expect(toggle('service.name', 'pay')).toBe('service: api');
    expect(toggle('message', 'x')).toBe('service: api');
  });

  it('remembers descriptions from query results', () => {
    const ds = new SLSDataSource(settings, passthrough);
    (ds as unknown as { learnFields: (f: unknown) => void }).learnFields(frame);
    // Grafana does not always pass the frame with ➕ / ➖.
    expect(ds.toggleQueryFilter(q, { type: 'FILTER_FOR', options: { key: 'host.name', value: 'web-1' } }).query).toBe(
      'service: api and __tag__:__hostname__: "web-1"'
    );
    expect(
      ds.queryHasFilter({ refId: 'A', query: '__tag__:__hostname__: "web-1"' }, { key: 'host.name', value: 'web-1' })
    ).toBe(true);
  });
});

describe('catalog requests', () => {
  it('uses the logstore of the dashboard queries and does not keep empty catalogs', async () => {
    const ds = new SLSDataSource(settings, passthrough);
    const post = jest.fn().mockResolvedValue({ fields: [], indexKnown: false });
    ds.postResource = post as never;
    await ds.getTagKeys({ queries: [{ refId: 'A', logstore: 'other' }], filters: [] } as never);
    expect(post).toHaveBeenLastCalledWith('api/catalog', { logstore: 'other', refresh: false });
    await ds.getTagKeys({ queries: [{ refId: 'A', logstore: 'other' }], filters: [] } as never);
    expect(post).toHaveBeenCalledTimes(2); // an empty catalog is asked again
  });
});
