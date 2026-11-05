import type { CatalogField } from '../types';
import { fieldDiagnostics } from './diagnostics';

const fields: CatalogField[] = [
  { name: 'level', indexed: true, stats: true, slsName: 'level' },
  { name: 'msg', indexed: false, stats: false, slsName: 'msg' },
  { name: 'host.name', indexed: true, stats: true, slsName: '__tag__:__hostname__', rule: 'rename' },
  { name: 'service.name', rule: 'extract' },
];

function hints(statement: string) {
  return fieldDiagnostics(statement, fields, true).map((d) => [
    statement.slice(d.start, d.end),
    d.message.split(' ')[2],
  ]);
}

describe('fieldDiagnostics', () => {
  it('flags search on fields without an index or with Grafana-only names', () => {
    expect(hints('level: ERROR and msg: "a: b"')).toEqual([['msg', 'no']]);
    expect(hints('host.name: web-1 or (service.name: pay)')).toEqual([
      ['host.name', 'the'],
      ['service.name', 'produced'],
    ]);
    expect(hints('__tag__:__hostname__: web-1')).toEqual([]);
  });

  it('flags SQL on fields without statistics, ignoring strings', () => {
    expect(hints(`* | select msg, "msg", level where level = 'msg' group by msg`)).toEqual([
      ['msg', 'no'],
      ['"msg"', 'no'],
      ['msg', 'no'],
    ]);
  });

  it('says nothing when the index is unknown', () => {
    expect(fieldDiagnostics('msg: a', fields, false)).toEqual([]);
  });
});
