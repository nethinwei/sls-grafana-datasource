import { Button, Combobox, type ComboboxOption, IconButton, Input, Stack, Text } from '@grafana/ui';
import React from 'react';

import { DEFAULT_FIELD_RULES, type FieldRule, type FieldRuleType, OTEL_FIELD_RULES } from '../types';

interface Props {
  /** undefined: the data source uses DEFAULT_FIELD_RULES. */
  value: FieldRule[] | undefined;
  onChange: (value: FieldRule[] | undefined) => void;
}

/** One parameter of a rule: a text input, a select, or a word between inputs (`→`, `=`). */
type Param =
  | { key: keyof FieldRule; placeholder: string; width: number }
  | { key: 'format'; options: Array<ComboboxOption<string>>; fallback: string; width: number }
  | { text: string };

interface RuleSchema {
  description: string;
  /** Parameters laid out as a sentence. */
  params: Param[];
  blank: FieldRule;
}

const ARROW: Param = { text: '→' };

/**
 * Every rule type and how it is edited. Adding a rule type in the backend (pkg/rules.go) takes one
 * entry here and in FieldRuleType.
 */
export const RULE_SCHEMAS: Record<FieldRuleType, RuleSchema> = {
  time: {
    description: 'Build a time from a seconds field plus an optional nanoseconds field',
    params: [
      { key: 'from', placeholder: 'seconds, e.g. __time__', width: 22 },
      { text: '+ ns' },
      { key: 'nanos', placeholder: 'optional, e.g. __time_ns_part__', width: 26 },
      ARROW,
      { key: 'to', placeholder: 'e.g. timestamp', width: 18 },
      {
        key: 'format',
        width: 14,
        fallback: 'rfc3339',
        options: [
          { value: 'rfc3339', label: 'RFC 3339' },
          { value: 'unix_ms', label: 'unix ms' },
          { value: 'unix_ns', label: 'unix ns' },
        ],
      },
    ],
    blank: { type: 'time', from: '', to: '' },
  },
  rename: {
    description: 'Show a field under another name; filters map back to SLS',
    params: [
      { key: 'from', placeholder: 'SLS field, e.g. __tag__:__hostname__', width: 32 },
      ARROW,
      { key: 'to', placeholder: 'e.g. host_name', width: 22 },
    ],
    blank: { type: 'rename', from: '', to: '' },
  },
  extract: {
    description: 'New field from a regular expression match',
    params: [
      { key: 'from', placeholder: 'source field', width: 22 },
      { key: 'pattern', placeholder: 'regex, e.g. /logs/(.+)\\.log', width: 26 },
      { key: 'template', placeholder: '$1', width: 10 },
      ARROW,
      { key: 'to', placeholder: 'e.g. service_name', width: 20 },
    ],
    blank: { type: 'extract', from: '', to: '', pattern: '' },
  },
  drop: {
    description: 'Hide a field',
    params: [{ key: 'field', placeholder: 'field, e.g. __topic__', width: 32 }],
    blank: { type: 'drop', field: '' },
  },
  default: {
    description: 'Value for a field that is absent or empty',
    params: [
      { key: 'to', placeholder: 'field, e.g. deployment.environment', width: 32 },
      { text: '=' },
      { key: 'value', placeholder: 'value', width: 20 },
    ],
    blank: { type: 'default', to: '', value: '' },
  },
  template: {
    description: 'New field from other fields: ${a}/${b}',
    params: [
      { key: 'template', placeholder: 'e.g. ${service}/${env}', width: 32 },
      ARROW,
      { key: 'to', placeholder: 'target field', width: 20 },
    ],
    blank: { type: 'template', to: '', template: '' },
  },
  json: {
    description: 'Flatten JSON objects into fields, as Loki | json: request → request_method, request_header_…',
    params: [{ key: 'from', placeholder: 'field; empty = every JSON field', width: 32 }],
    blank: { type: 'json' },
  },
};

const TYPES: Array<ComboboxOption<FieldRuleType>> = (Object.keys(RULE_SCHEMAS) as FieldRuleType[]).map((t) => ({
  value: t,
  label: t,
  description: RULE_SCHEMAS[t].description,
}));

function RuleParams({
  rule,
  update,
  index,
}: {
  rule: FieldRule;
  update: (p: Partial<FieldRule>) => void;
  index: number;
}) {
  return (
    <>
      {(
        RULE_SCHEMAS[rule.type]?.params ?? [{ text: `unknown rule type "${rule.type}": choose a type or remove it` }]
      ).map((p, i) => {
        if ('text' in p) {
          return (
            <Text key={i} color="secondary">
              {p.text}
            </Text>
          );
        }
        if ('options' in p) {
          return (
            <Combobox
              key={i}
              width={p.width}
              aria-label={`${p.key} ${index + 1}`}
              options={p.options}
              value={(rule[p.key] as string | undefined) ?? p.fallback}
              onChange={(o) => update({ [p.key]: o.value === p.fallback ? undefined : o.value } as Partial<FieldRule>)}
            />
          );
        }
        return (
          <Input
            key={i}
            width={p.width}
            placeholder={p.placeholder}
            aria-label={`${p.key} ${index + 1}`}
            value={(rule[p.key] as string | undefined) ?? ''}
            onChange={(e) => update({ [p.key]: e.currentTarget.value })}
          />
        );
      })}
    </>
  );
}

/** Ordered list of field rules (see the README for every type). */
export function FieldRulesEditor({ value, onChange }: Props) {
  const isDefault = value === undefined;
  const rules = value ?? DEFAULT_FIELD_RULES;
  const set = (next: FieldRule[]) => onChange(next);
  const update = (i: number, patch: Partial<FieldRule>) => set(rules.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const move = (i: number, d: -1 | 1) => {
    const next = [...rules];
    [next[i], next[i + d]] = [next[i + d], next[i]];
    set(next);
  };
  const addOtel = () => {
    const existing = new Set(rules.map((r) => `${r.type}:${r.from}`));
    set([...rules, ...OTEL_FIELD_RULES.filter((r) => !existing.has(`${r.type}:${r.from}`))]);
  };

  return (
    <Stack direction="column" gap={1}>
      <Text color="secondary">
        Rules run in order on every query result and only change what Grafana shows. Fields renamed by{' '}
        <code>rename</code> (filters use the SLS name) and flattened by <code>json</code> (filters read the JSON) can be
        filtered on; fields produced by the other rules exist only in Grafana. <code>__tag__:__pack_id__</code> and{' '}
        <code>__pack_meta__</code> are needed by log context and cannot be renamed or dropped.
        {isDefault && ' Showing the default rules; editing them saves your own list.'}
      </Text>
      {rules.map((rule, i) => (
        <Stack key={i} direction="row" gap={1} alignItems="center" wrap="wrap">
          <Combobox
            width={12}
            aria-label={`Rule ${i + 1} type`}
            options={TYPES}
            value={rule.type}
            onChange={(o) => set(rules.map((r, j) => (j === i ? RULE_SCHEMAS[o.value].blank : r)))}
          />
          <RuleParams rule={rule} index={i} update={(p) => update(i, p)} />
          <IconButton name="arrow-up" tooltip="Move up" disabled={i === 0} onClick={() => move(i, -1)} />
          <IconButton
            name="arrow-down"
            tooltip="Move down"
            disabled={i === rules.length - 1}
            onClick={() => move(i, 1)}
          />
          <IconButton name="trash-alt" tooltip="Remove" onClick={() => set(rules.filter((_, j) => j !== i))} />
        </Stack>
      ))}
      <Stack direction="row" gap={1}>
        <Button variant="secondary" size="sm" icon="plus" onClick={() => set([...rules, RULE_SCHEMAS.rename.blank])}>
          Add rule
        </Button>
        <Button variant="secondary" size="sm" onClick={addOtel}>
          Add OpenTelemetry renames
        </Button>
        {!isDefault && (
          <Button variant="secondary" size="sm" fill="text" onClick={() => onChange(undefined)}>
            Reset to defaults
          </Button>
        )}
      </Stack>
    </Stack>
  );
}
