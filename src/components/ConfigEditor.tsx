import type { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { Field, Input, SecretInput, Stack, Switch, TagsInput, Text, TextLink } from '@grafana/ui';
import React from 'react';

import { DEFAULT_TIME_FIELD, type LogsConfig, type SLSDataSourceOptions, type SLSSecureJsonData } from '../types';
import { FieldRulesEditor } from './FieldRulesEditor';
import { LineFieldsEditor } from './LineFieldsEditor';

type Props = DataSourcePluginOptionsEditorProps<SLSDataSourceOptions, SLSSecureJsonData>;

export function ConfigEditor({ options, onOptionsChange }: Props) {
  const { jsonData, secureJsonFields } = options;
  const secure = options.secureJsonData ?? {};

  const setJson = (patch: Partial<SLSDataSourceOptions>) =>
    onOptionsChange({ ...options, jsonData: { ...jsonData, ...patch } });
  const logs = jsonData.logs ?? {};
  /** Sets logs settings; undefined values are removed so the defaults apply. */
  const setLogs = (patch: Partial<LogsConfig>) => {
    const next: LogsConfig = { ...logs, ...patch };
    (Object.keys(next) as Array<keyof LogsConfig>).forEach((k) => next[k] === undefined && delete next[k]);
    setJson({ logs: Object.keys(next).length ? next : undefined });
  };
  const setLine = (patch: NonNullable<LogsConfig['line']>) => {
    const line = { ...logs.line, ...patch };
    setLogs({ line: line.fields?.length || line.exclude?.length || line.rest === false ? line : undefined });
  };
  const setSecret = (key: keyof SLSSecureJsonData, value: string) =>
    onOptionsChange({ ...options, secureJsonData: { ...secure, [key]: value } });
  const resetSecret = (key: keyof SLSSecureJsonData) =>
    onOptionsChange({
      ...options,
      secureJsonFields: { ...secureJsonFields, [key]: false },
      secureJsonData: { ...secure, [key]: '' },
    });

  return (
    <Stack direction="column" gap={2}>
      <Text element="h3">Connection</Text>
      <Field
        label="Endpoint"
        description="SLS endpoint of the project's region, e.g. cn-hangzhou.log.aliyuncs.com (use the -intranet endpoint inside Alibaba Cloud)."
        required
      >
        <Input
          width={60}
          value={options.url}
          placeholder="cn-hangzhou.log.aliyuncs.com"
          onChange={(e) => onOptionsChange({ ...options, url: e.currentTarget.value.trim() })}
        />
      </Field>
      <Field label="Project" required>
        <Input
          width={40}
          value={jsonData.project ?? ''}
          onChange={(e) => setJson({ project: e.currentTarget.value.trim() })}
        />
      </Field>
      <Field
        label="Default logstore"
        description="Used by queries that do not choose a logstore, and by the health check."
      >
        <Input
          width={40}
          value={jsonData.logstore ?? ''}
          onChange={(e) => setJson({ logstore: e.currentTarget.value.trim() })}
        />
      </Field>
      <Field
        label="Region"
        description="Region ID such as cn-hangzhou. When set, requests are signed with signature V4."
      >
        <Input
          width={30}
          value={jsonData.region ?? ''}
          onChange={(e) => setJson({ region: e.currentTarget.value.trim() })}
        />
      </Field>

      <Text element="h3">Authentication</Text>
      <Text color="secondary">
        Use a RAM user with read access to the logstores only: <code>log:GetLogStoreLogs</code> is enough for queries;{' '}
        <code>log:GetLogStore</code>, <code>log:ListLogStores</code> and <code>log:GetIndex</code> improve the health
        check, the logstore list and field suggestions.{' '}
        <TextLink href="https://help.aliyun.com/zh/sls/user-guide/overview-of-ram-policies" external>
          RAM policies
        </TextLink>
      </Text>
      <Field label="AccessKey ID" required>
        <SecretInput
          width={50}
          isConfigured={Boolean(secureJsonFields?.accessKeyId)}
          value={secure.accessKeyId ?? ''}
          onChange={(e) => setSecret('accessKeyId', e.currentTarget.value)}
          onReset={() => resetSecret('accessKeyId')}
        />
      </Field>
      <Field label="AccessKey secret" required>
        <SecretInput
          width={50}
          isConfigured={Boolean(secureJsonFields?.accessKeySecret)}
          value={secure.accessKeySecret ?? ''}
          onChange={(e) => setSecret('accessKeySecret', e.currentTarget.value)}
          onReset={() => resetSecret('accessKeySecret')}
        />
      </Field>

      <Text element="h3">Advanced</Text>
      <Field
        label="SQL time zone"
        description="Time zone of date-time strings returned by SQL (timestamp columns, from_unixtime, date_format). SLS uses UTC+8."
      >
        <Input
          width={30}
          placeholder="Asia/Shanghai"
          value={jsonData.sqlTimeZone ?? ''}
          onChange={(e) => setJson({ sqlTimeZone: e.currentTarget.value.trim() || undefined })}
        />
      </Field>
      <Field label="International site" description="Open the console on alibabacloud.com instead of aliyun.com.">
        <Switch
          value={Boolean(jsonData.international)}
          onChange={(e) => setJson({ international: e.currentTarget.checked })}
        />
      </Field>

      <Text element="h3">Logs</Text>
      <Field
        label="Level field"
        description="Field holding the log level (after the rules), used for level colours and the logs volume. Enable SQL statistics on it to split the volume by level."
      >
        <Input
          width={30}
          placeholder="level"
          value={logs.levelField ?? ''}
          onChange={(e) => setLogs({ levelField: e.currentTarget.value.trim() || undefined })}
        />
      </Field>
      <Field
        label="Time field"
        description="Field that becomes the row timestamp, written by a time rule (see the default rules)."
      >
        <Input
          width={30}
          placeholder={DEFAULT_TIME_FIELD}
          value={logs.timeField ?? ''}
          onChange={(e) => setLogs({ timeField: e.currentTarget.value.trim() || undefined })}
        />
      </Field>
      <Field
        label="Line fields"
        description="Fields opening the log line, in this order. Listing msg last makes it the boundary: standard fields (service, host, trace) before it, the application's own fields after it. Fields missing from a log are skipped; fields produced by rules appear only when listed here."
      >
        <LineFieldsEditor
          value={logs.line?.fields ?? []}
          onChange={(fields) => setLine({ fields: fields.length ? fields : undefined })}
        />
      </Field>
      <Field label="Hidden from line" description="Fields never shown in the log line; they stay in the log details.">
        <TagsInput
          width={60}
          placeholder="field name, Enter to add"
          tags={logs.line?.exclude ?? []}
          onChange={(exclude) => setLine({ exclude: exclude.length ? exclude : undefined })}
        />
      </Field>
      <Field
        label="Other fields in line"
        description="Append the other business fields (not SLS meta fields) after the line fields."
      >
        <Switch
          aria-label="Other fields in line"
          value={logs.line?.rest !== false}
          onChange={(e) => setLine({ rest: e.currentTarget.checked ? undefined : false })}
        />
      </Field>
      <Field label="Field rules">
        <FieldRulesEditor value={logs.rules} onChange={(rules) => setLogs({ rules })} />
      </Field>
    </Stack>
  );
}
