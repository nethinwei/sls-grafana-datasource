import { css } from '@emotion/css';
import type { GrafanaTheme2 } from '@grafana/data';
import { Button, Icon, Input, Modal, Stack, Text, Tooltip, useStyles2 } from '@grafana/ui';
import React, { useMemo, useState } from 'react';

import type { Catalog, CatalogField } from '../types';

interface Props {
  catalog?: Catalog;
  loading: boolean;
  onRefresh: () => void;
  onDismiss: () => void;
}

function Flag({ value, yes, no }: { value: boolean | undefined; yes: string; no: string }) {
  if (value === undefined) {
    return <Text color="secondary">?</Text>;
  }
  return (
    <Tooltip content={value ? yes : no}>
      <Icon name={value ? 'check' : 'times'} aria-label={value ? yes : no} />
    </Tooltip>
  );
}

function origin(f: CatalogField): string {
  if (!f.rule) {
    return '';
  }
  return f.slsName ? `${f.rule} of ${f.slsName}` : `${f.rule} rule (Grafana only)`;
}

/**
 * The logstore's fields with what can be done with each: search (field index), SQL (statistics),
 * and where the field comes from when a rule renamed or produced it.
 */
export function FieldBrowser({ catalog, loading, onRefresh, onDismiss }: Props) {
  const styles = useStyles2(getStyles);
  const [filter, setFilter] = useState('');
  const fields = useMemo(() => {
    const f = filter.toLowerCase();
    return (catalog?.fields ?? []).filter(
      (x) => !f || x.name.toLowerCase().includes(f) || x.slsName?.toLowerCase().includes(f)
    );
  }, [catalog, filter]);

  return (
    <Modal title="Fields" isOpen onDismiss={onDismiss} className={styles.modal}>
      <Stack direction="column" gap={1}>
        <Stack direction="row" gap={1} alignItems="center">
          <Input
            width={30}
            prefix={<Icon name="search" />}
            placeholder="Filter fields"
            value={filter}
            onChange={(e) => setFilter(e.currentTarget.value)}
          />
          <Button variant="secondary" size="md" icon="sync" disabled={loading} onClick={onRefresh}>
            Refresh
          </Button>
          <Text color="secondary">
            {catalog?.message ??
              (catalog?.indexKnown
                ? 'From the logstore index and recent logs.'
                : 'The index could not be read (log:GetIndex); fields come from recent logs.')}
          </Text>
        </Stack>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>Field</th>
              <th>Type</th>
              <th>Search</th>
              <th>SQL</th>
              <th>From</th>
            </tr>
          </thead>
          <tbody>
            {fields.map((f) => (
              <tr key={f.name}>
                <td>
                  <code>{f.name}</code>
                </td>
                <td>{f.type ?? ''}</td>
                <td>
                  {f.slsName ? (
                    <Flag value={f.indexed} yes="Field index: field: value works" no="No index: use | where" />
                  ) : (
                    '-'
                  )}
                </td>
                <td>
                  {f.slsName ? <Flag value={f.stats} yes="SQL statistics enabled" no="No SQL statistics" /> : '-'}
                </td>
                <td>{origin(f)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Stack>
    </Modal>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  modal: css({ width: '90vw', maxWidth: 1000 }),
  table: css({
    width: '100%',
    borderCollapse: 'collapse',
    'th, td': {
      padding: theme.spacing(0.5, 1),
      borderBottom: `1px solid ${theme.colors.border.weak}`,
      textAlign: 'left',
    },
    th: { color: theme.colors.text.secondary, fontWeight: theme.typography.fontWeightMedium },
  }),
});
