import { Button, IconButton, Input, Stack } from '@grafana/ui';
import React from 'react';

interface Props {
  value: string[];
  onChange: (value: string[]) => void;
}

/** Ordered list of the fields opening the log line. */
export function LineFieldsEditor({ value, onChange }: Props) {
  const move = (i: number, d: -1 | 1) => {
    const next = [...value];
    [next[i], next[i + d]] = [next[i + d], next[i]];
    onChange(next);
  };
  return (
    <Stack direction="column" gap={1}>
      {value.map((field, i) => (
        <Stack key={i} direction="row" gap={1} alignItems="center">
          <Input
            width={30}
            aria-label={`Line field ${i + 1}`}
            placeholder="field, e.g. service_name"
            value={field}
            onChange={(e) => onChange(value.map((f, j) => (j === i ? e.currentTarget.value.trim() : f)))}
          />
          <IconButton name="arrow-up" tooltip="Move up" disabled={i === 0} onClick={() => move(i, -1)} />
          <IconButton
            name="arrow-down"
            tooltip="Move down"
            disabled={i === value.length - 1}
            onClick={() => move(i, 1)}
          />
          <IconButton name="trash-alt" tooltip="Remove" onClick={() => onChange(value.filter((_, j) => j !== i))} />
        </Stack>
      ))}
      <div>
        <Button variant="secondary" size="sm" icon="plus" onClick={() => onChange([...value, ''])}>
          Add field
        </Button>
      </div>
    </Stack>
  );
}
