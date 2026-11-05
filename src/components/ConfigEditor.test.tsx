import type { DataSourceSettings } from '@grafana/data';
import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';

import type { SLSDataSourceOptions, SLSSecureJsonData } from '../types';
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
