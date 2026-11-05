import { DataSourcePlugin } from '@grafana/data';

import { ConfigEditor } from './components/ConfigEditor';
import { QueryEditor } from './components/QueryEditor';
import { SLSDataSource } from './datasource';
import type { SLSDataSourceOptions, SLSQuery } from './types';

export const plugin = new DataSourcePlugin<SLSDataSource, SLSQuery, SLSDataSourceOptions>(SLSDataSource)
  .setConfigEditor(ConfigEditor)
  .setQueryEditor(QueryEditor);
