import type { DataSourceSettings } from '@grafana/data';
import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';

import { DEFAULT_FIELD_RULES, OTEL_FIELD_RULES, type SLSDataSourceOptions, type SLSSecureJsonData } from '../types';
import { ConfigEditor } from './ConfigEditor';

function options(): DataSourceSettings<SLSDataSourceOptions, SLSSecureJsonData> {
  return {
    url: 'cn-hangzhou.log.aliyuncs.com',
    jsonData: { project: 'p1', logstore: 'ls1' },
    secureJsonFields: { accessKeyId: true, accessKeySecret: true },
  } as unknown as DataSourceSettings<SLSDataSourceOptions, SLSSecureJsonData>;
}

describe('ConfigEditor', () => {
  it('renders every setting', () => {
    render(<ConfigEditor options={options()} onOptionsChange={jest.fn()} />);
    for (const label of [
      'Endpoint',
      'Project',
      'Default logstore',
      'Region',
      'AccessKey ID',
      'AccessKey secret',
      'SQL time zone',
      'Level field',
      'International site',
    ]) {
      // Required fields append " *" in a separate element.
      expect(screen.getByText(new RegExp(`^${label}( \\*)?$`))).toBeInTheDocument();
    }
    expect(screen.getByDisplayValue('cn-hangzhou.log.aliyuncs.com')).toBeInTheDocument();
    expect(screen.getByDisplayValue('p1')).toBeInTheDocument();
    // Configured secrets are not shown, only a reset button.
    expect(screen.getAllByRole('button', { name: 'Reset' })).toHaveLength(2);
  });

  it('updates jsonData and resets secrets', () => {
    const onChange = jest.fn();
    render(<ConfigEditor options={options()} onOptionsChange={onChange} />);

    fireEvent.change(screen.getByDisplayValue('p1'), { target: { value: ' p2 ' } });
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ jsonData: { project: 'p2', logstore: 'ls1' } })
    );

    fireEvent.click(screen.getAllByRole('button', { name: 'Reset' })[0]);
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({
        secureJsonFields: { accessKeyId: false, accessKeySecret: true },
        secureJsonData: { accessKeyId: '' },
      })
    );
  });
});

describe('ConfigEditor logs', () => {
  const lastJson = (fn: jest.Mock) => fn.mock.calls[fn.mock.calls.length - 1][0].jsonData as SLSDataSourceOptions;

  it('edits the default rules into an own list and resets them', () => {
    const onChange = jest.fn();
    const { rerender } = render(<ConfigEditor options={options()} onOptionsChange={onChange} />);
    expect(screen.getByLabelText('nanos 1')).toHaveValue('__time_ns_part__'); // the default time rule

    fireEvent.click(screen.getByRole('button', { name: 'Add OpenTelemetry renames' }));
    const rules = lastJson(onChange).logs?.rules ?? [];
    expect(rules.slice(0, 3)).toEqual(DEFAULT_FIELD_RULES);
    expect(rules.slice(3)).toEqual(OTEL_FIELD_RULES);

    const opts = options();
    opts.jsonData.logs = { rules };
    rerender(<ConfigEditor options={opts} onOptionsChange={onChange} />);
    fireEvent.change(screen.getByLabelText('to 4'), { target: { value: 'node' } });
    expect(lastJson(onChange).logs?.rules?.[3]).toEqual({ type: 'rename', from: '__tag__:__hostname__', to: 'node' });

    fireEvent.click(screen.getByRole('button', { name: 'Reset to defaults' }));
    expect(lastJson(onChange).logs).toBeUndefined();
  });

  it('sets the level field and the line', () => {
    const onChange = jest.fn();
    render(<ConfigEditor options={options()} onOptionsChange={onChange} />);
    fireEvent.change(screen.getByPlaceholderText('level'), { target: { value: 'severity' } });
    expect(lastJson(onChange).logs).toEqual({ levelField: 'severity' });

    fireEvent.click(screen.getByLabelText('Other fields in line'));
    expect(lastJson(onChange).logs).toEqual({ line: { rest: false } });
  });
});
