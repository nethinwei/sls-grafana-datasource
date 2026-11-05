import type { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { Field, Input, SecretInput, Stack, Switch, Text, TextLink } from '@grafana/ui';
import React from 'react';

import type { SLSDataSourceOptions, SLSSecureJsonData } from '../types';

type Props = DataSourcePluginOptionsEditorProps<SLSDataSourceOptions, SLSSecureJsonData>;

export function ConfigEditor({ options, onOptionsChange }: Props) {
  const { jsonData, secureJsonFields } = options;
  const secure = options.secureJsonData ?? {};

  const setJson = (patch: Partial<SLSDataSourceOptions>) =>
    onOptionsChange({ ...options, jsonData: { ...jsonData, ...patch } });
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
    </Stack>
  );
}
