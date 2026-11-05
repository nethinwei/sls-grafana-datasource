import type { ScopedVars } from '@grafana/data';
import type { TemplateSrv } from '@grafana/runtime';

import { interpolateLogstore, interpolateQuery } from './templating';

type Var = {
  name: string;
  label?: string;
  description?: string;
  multi?: boolean;
  includeAll?: boolean;
  current: string[] | string;
};

/** Minimal TemplateSrv: $name / ${name} / ${name:json}, with an optional format function. */
function fakeTemplateSrv(vars: Var[]): TemplateSrv {
  const byName = new Map(vars.map((v) => [v.name, v]));
  return {
    getVariables: () => vars as never,
    replace(target = '', scopedVars?: ScopedVars, format?: unknown) {
      return target.replace(/\$\{(\w+)(?::(\w+))?\}|\$(\w+)/g, (m, braced, fmt, bare) => {
        const name = braced ?? bare;
        const scoped = scopedVars?.[name];
        const v = byName.get(name);
        if (!v && !scoped) {
          return m;
        }
        const value = scoped ? scoped.value : v!.current;
        if (fmt === 'json') {
          return JSON.stringify(value);
        }
        if (typeof format === 'function') {
          return format(value, v);
        }
        return Array.isArray(value) ? value.join(',') : String(value);
      });
    },
  } as unknown as TemplateSrv;
}

describe('interpolateQuery', () => {
  const srv = fakeTemplateSrv([
    { name: 'service_name', multi: true, includeAll: true, current: ['topup', 'cashier'] },
    { name: 'one', multi: true, current: ['only'] },
    { name: 'plain', multi: true, current: ['a', 'b'] },
    { name: 'single', current: 'checkout' },
    { name: 'all', includeAll: true, current: '*' },
    { name: 'quoted', multi: true, current: ['say "hi"', 'x'] },
  ]);

  it.each([
    [
      'service_name: $service_name and level: ERROR',
      '(service_name: "topup" or service_name: "cashier") and level: ERROR',
    ],
    ['service_name:${service_name}', '(service_name: "topup" or service_name: "cashier")'],
    ['__tag__:__path__: $one', '__tag__:__path__: "only"'],
    ['service_name: $all', 'service_name: *'],
    ['msg: $quoted', '(msg: "say \\"hi\\"" or msg: "x")'],
    // Not "key: $var": values joined with OR.
    ['($plain)', '(a OR b)'],
    // Single-value variables are replaced as they are.
    ['channel: $single', 'channel: checkout'],
    // SQL with an explicit format is untouched by the expansion.
    [
      '* | select count(*) from log where service_name in (${service_name:json})',
      '* | select count(*) from log where service_name in (["topup","cashier"])',
    ],
  ])('%s', (input, want) => {
    expect(interpolateQuery(input, {}, srv)).toBe(want);
  });

  it('uses the scoped value of repeated panels instead of expanding', () => {
    const scoped = { service_name: { text: 'topup', value: 'topup' } };
    expect(interpolateQuery('service_name: $service_name', scoped, srv)).toBe('service_name: topup');
  });
});

describe('interpolateLogstore', () => {
  const srv = fakeTemplateSrv([{ name: 'ls', current: 'app-logs' }]);
  it.each([
    [undefined, ''],
    ['plain', 'plain'],
    ['$ls', 'app-logs'],
  ])('%s', (input, want) => {
    expect(interpolateLogstore(input, {}, srv)).toBe(want);
  });
});
