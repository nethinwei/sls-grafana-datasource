import type { QueryEditorProps } from '@grafana/data';
import { InlineField, Input, Stack } from '@grafana/ui';
import React from 'react';

import { toVariableQuery, type SLSDataSource } from '../datasource';
import type { SLSDataSourceOptions, SLSQuery } from '../types';
import { SLSQueryField } from './SLSQueryField';

type Props = QueryEditorProps<SLSDataSource, SLSQuery, SLSDataSourceOptions>;

/**
 * Variable query: usually SQL whose first column holds the values, e.g.
 * `* | select service_name from log group by service_name limit 500`.
 * A second column named `text` / `value` sets display text and value separately.
 */
export function VariableQueryEditor({ datasource, query, onChange }: Props) {
  const q = toVariableQuery(query);
  return (
    <Stack direction="column" gap={1}>
      <InlineField label="Logstore" labelWidth={12} tooltip="Empty uses the data source default.">
        <Input
          width={36}
          placeholder={datasource.defaultLogstore}
          defaultValue={q.logstore}
          onBlur={(e) => onChange({ ...q, logstore: e.currentTarget.value })}
        />
      </InlineField>
      <SLSQueryField value={q.query ?? ''} height={60} onChange={(v) => onChange({ ...q, query: v })} />
    </Stack>
  );
}
