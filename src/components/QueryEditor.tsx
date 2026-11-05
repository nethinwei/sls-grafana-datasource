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
import {
  type Catalog,
  DEFAULT_MAX_LINES,
  MAX_MAX_LINES,
  type QueryFormat,
  type SLSDataSourceOptions,
  type SLSQuery,
} from '../types';
import { FieldBrowser } from './FieldBrowser';
import { SLSQueryField } from './SLSQueryField';

type Props = QueryEditorProps<SLSDataSource, SLSQuery, SLSDataSourceOptions>;

const FORMATS: Array<ComboboxOption<QueryFormat>> = [
  { value: 'time_series', label: 'Time series', description: 'Results with the time column become time series' },
  { value: 'table', label: 'Table', description: 'The SQL result as it is' },
];

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
    let current = true; // a slower answer for a previous logstore must not overwrite this one
    datasource.getCatalog(catalogLogstore).then(
      (c) => current && setCatalog(c),
      () => current && setCatalog(undefined)
    );
    return () => {
      current = false;
    };
  }, [datasource, catalogLogstore]);
  const refreshCatalog = () => {
    setCatalogLoading(true);
    datasource
      .getCatalog(catalogLogstore, true)
      .then(setCatalog, () => setCatalog(undefined))
      .finally(() => setCatalogLoading(false));
  };

  const update = (patch: Partial<SLSQuery>, run = false) => {
    onChange({ ...query, ...patch });
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
  const logstore = query.logstore ?? '';
  const format = query.format ?? 'time_series';

  const openConsole = async () => {
    setConsoleError(undefined);
    if (!range) {
      return;
    }
    // Open the window within the click: browsers block windows opened after an await.
    const win = window.open('', '_blank');
    try {
      const url = await datasource.getConsoleUrl(query, range);
      if (win) {
        win.opener = null;
        win.location.href = url;
      }
    } catch (e) {
      win?.close();
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
            defaultValue={query.maxLines}
            onBlur={(e) => {
              const maxLines = e.currentTarget.value ? Number(e.currentTarget.value) : undefined;
              if (maxLines !== query.maxLines) {
                update({ maxLines }, true);
              }
            }}
          />
        </InlineField>
        <InlineField label="Format" tooltip="How SQL results are returned. Search and SPL always return logs.">
          <Combobox<QueryFormat>
            width={16}
            options={FORMATS}
            value={format}
            onChange={(o) => update({ format: o.value === 'time_series' ? undefined : o.value }, true)}
          />
        </InlineField>
        {format === 'time_series' && (
          <InlineField
            label="Time column"
            labelWidth={16}
            tooltip="SQL column holding the time. With a time column the result becomes time series, one per combination of string columns."
          >
            <Input
              width={14}
              placeholder="time"
              defaultValue={query.timeColumn}
              onBlur={(e) => {
                const timeColumn = e.currentTarget.value || undefined;
                if (timeColumn !== query.timeColumn) {
                  update({ timeColumn }, true);
                }
              }}
            />
          </InlineField>
        )}
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
