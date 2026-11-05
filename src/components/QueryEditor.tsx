import type { QueryEditorProps } from '@grafana/data';
import { getTemplateSrv } from '@grafana/runtime';
import {
  Button,
  Combobox,
  type ComboboxOption,
  InlineField,
  InlineFieldRow,
  InlineSwitch,
  Input,
  Stack,
} from '@grafana/ui';
import React, { useEffect, useState } from 'react';

import type { SLSDataSource } from '../datasource';
import { type Catalog, DEFAULT_MAX_LINES, MAX_MAX_LINES, type SLSDataSourceOptions, type SLSQuery } from '../types';
import { FieldBrowser } from './FieldBrowser';
import { SLSQueryField } from './SLSQueryField';

type Props = QueryEditorProps<SLSDataSource, SLSQuery, SLSDataSourceOptions>;

// Fields of plugin 2.x queries that the backend no longer uses; dropped on the first edit.
const LEGACY_FIELDS = [
  'xcol',
  'ycol',
  'logsPerPage',
  'currentPage',
  'totalLogs',
  'type',
  'legendFormat',
  'step',
  'queryType',
];

function clean(query: SLSQuery): SLSQuery {
  const q: Record<string, unknown> = { ...query };
  // xcol naming a column was the time column; keep that meaning.
  const xcol = q.xcol;
  if (
    !q.timeColumn &&
    typeof xcol === 'string' &&
    xcol &&
    !['table', 'bar', 'pie', 'map', 'stat', 'trace'].includes(xcol)
  ) {
    q.timeColumn = xcol;
  }
  if (!q.maxLines && typeof q.totalLogs === 'number') {
    q.maxLines = q.totalLogs;
  }
  LEGACY_FIELDS.forEach((f) => delete q[f]);
  return q as unknown as SLSQuery;
}

export function QueryEditor({ datasource, query, onChange, onRunQuery, range }: Props) {
  const [logstores, setLogstores] = useState<string[]>([]);
  const [consoleError, setConsoleError] = useState<string>();

  useEffect(() => {
    datasource.getLogstores().then(setLogstores, () => setLogstores([]));
  }, [datasource]);

  // Field catalog of the chosen logstore: completion, hints and the field browser.
  const [catalog, setCatalog] = useState<Catalog>();
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [browsing, setBrowsing] = useState(false);
  const catalogLogstore = query.logstore?.startsWith('$') ? undefined : query.logstore || undefined;
  useEffect(() => {
    datasource.getCatalog(catalogLogstore).then(setCatalog, () => setCatalog(undefined));
  }, [datasource, catalogLogstore]);
  const refreshCatalog = () => {
    setCatalogLoading(true);
    datasource
      .getCatalog(catalogLogstore, true)
      .then(setCatalog, () => setCatalog(undefined))
      .finally(() => setCatalogLoading(false));
  };

  const update = (patch: Partial<SLSQuery>, run = false) => {
    onChange({ ...clean(query), ...patch });
    if (run) {
      onRunQuery();
    }
  };

  const variableOptions = getTemplateSrv()
    .getVariables()
    .map((v) => ({ label: `$${v.name}`, value: `$${v.name}` }));
  const logstoreOptions: Array<ComboboxOption<string>> = [
    ...logstores.map((l) => ({ label: l, value: l })),
    ...variableOptions,
  ];
  const logstore = query.logstore?.replace(/^__custom__@/, '$') ?? '';
  const cleaned = clean(query);

  const openConsole = async () => {
    setConsoleError(undefined);
    if (!range) {
      return;
    }
    try {
      window.open(await datasource.getConsoleUrl(query, range), '_blank', 'noopener');
    } catch (e) {
      setConsoleError(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <Stack direction="column" gap={1}>
      <InlineFieldRow>
        <InlineField
          label="Logstore"
          labelWidth={12}
          tooltip="Empty uses the data source default. Variables are allowed."
        >
          <Combobox<string>
            width={36}
            options={logstoreOptions}
            value={logstore || null}
            placeholder={datasource.defaultLogstore || 'choose a logstore'}
            createCustomValue
            isClearable
            onChange={(v) => update({ logstore: v?.value ?? '' }, true)}
          />
        </InlineField>
        <Button
          variant="secondary"
          size="md"
          icon="external-link-alt"
          onClick={openConsole}
          tooltip={consoleError ?? 'Open this query in the SLS console'}
        >
          SLS console
        </Button>
        <Button
          variant="secondary"
          size="md"
          icon="list-ul"
          onClick={() => setBrowsing(true)}
          tooltip="Fields of the logstore: index, SQL statistics and field rules"
        >
          Fields
        </Button>
      </InlineFieldRow>
      {browsing && (
        <FieldBrowser
          catalog={catalog}
          loading={catalogLoading}
          onRefresh={refreshCatalog}
          onDismiss={() => setBrowsing(false)}
        />
      )}

      <SLSQueryField
        value={query.query ?? ''}
        catalog={catalog}
        onChange={(v) => update({ query: v })}
        onRunQuery={onRunQuery}
      />

      <InlineFieldRow>
        <InlineField
          label="Max lines"
          labelWidth={12}
          tooltip={`Log rows returned by search queries (1-${MAX_MAX_LINES}). SQL is limited by its LIMIT.`}
        >
          <Input
            type="number"
            width={12}
            min={1}
            max={MAX_MAX_LINES}
            placeholder={String(DEFAULT_MAX_LINES)}
            defaultValue={cleaned.maxLines}
            onBlur={(e) =>
              update({ maxLines: e.currentTarget.value ? Number(e.currentTarget.value) : undefined }, true)
            }
          />
        </InlineField>
        <InlineField
          label="Time column"
          labelWidth={16}
          tooltip="SQL column holding the time. With a time column the result becomes time series, one per combination of string columns."
        >
          <Input
            width={14}
            placeholder="time"
            defaultValue={cleaned.timeColumn}
            onBlur={(e) => update({ timeColumn: e.currentTarget.value || undefined }, true)}
          />
        </InlineField>
        <InlineField
          label="Dedicated SQL"
          tooltip="Run SQL on the SLS Dedicated SQL engine (more resources, billed separately)."
        >
          <InlineSwitch
            value={Boolean(query.powerSql)}
            onChange={(e) => update({ powerSql: e.currentTarget.checked }, true)}
          />
        </InlineField>
      </InlineFieldRow>
    </Stack>
  );
}
