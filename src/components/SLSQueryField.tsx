import { CodeEditor, type Monaco, type monacoTypes } from '@grafana/ui';
import React, { useCallback, useEffect, useRef, useState } from 'react';

import type { Catalog } from '../types';
import { getCompletionProvider, setModelFields } from '../language/completion';
import { fieldDiagnostics } from '../language/diagnostics';
import { slsLanguageDefinition } from '../language/definition';
import { language, languageConfiguration } from '../language/sls';

let languageRegistered = false;

function registerLanguage(monaco: Monaco) {
  if (languageRegistered) {
    return;
  }
  languageRegistered = true;
  const { id, aliases, extensions, mimetypes } = slsLanguageDefinition;
  monaco.languages.register({ id, aliases, extensions, mimetypes });
  monaco.languages.setMonarchTokensProvider(id, language);
  monaco.languages.setLanguageConfiguration(id, languageConfiguration);
  monaco.languages.registerCompletionItemProvider(id, getCompletionProvider(monaco));
}

interface Props {
  value: string;
  /** Called when the editor loses focus or the user presses Shift+Enter. */
  onChange: (value: string) => void;
  onRunQuery?: () => void;
  height?: number;
  /** Field catalog of the logstore: completion and hints on fields. */
  catalog?: Catalog;
}

const MARKER_OWNER = 'sls-fields';

/** SLS search / SPL / SQL editor with syntax highlighting and completion. */
export function SLSQueryField({ value, onChange, onRunQuery, height = 80, catalog }: Props) {
  // The Shift+Enter command is registered once on mount; read the latest callbacks through refs.
  const latest = useRef({ onChange, onRunQuery });
  useEffect(() => {
    latest.current = { onChange, onRunQuery };
  }, [onChange, onRunQuery]);

  const [editor, setEditor] = useState<{ ed: monacoTypes.editor.IStandaloneCodeEditor; monaco: Monaco }>();
  useEffect(() => {
    const model = editor?.ed.getModel();
    if (!editor || !model) {
      return;
    }
    const uri = model.uri.toString();
    setModelFields(uri, catalog?.fields);
    // Hints follow the text as it is typed.
    const mark = () => {
      const markers = fieldDiagnostics(model.getValue(), catalog?.fields ?? [], Boolean(catalog?.indexKnown)).map(
        (d) => {
          const start = model.getPositionAt(d.start);
          const end = model.getPositionAt(d.end);
          return {
            severity: editor.monaco.MarkerSeverity.Warning,
            message: d.message,
            startLineNumber: start.lineNumber,
            startColumn: start.column,
            endLineNumber: end.lineNumber,
            endColumn: end.column,
          };
        }
      );
      editor.monaco.editor.setModelMarkers(model, MARKER_OWNER, markers);
    };
    mark();
    const sub = model.onDidChangeContent(mark);
    return () => {
      sub.dispose();
      setModelFields(uri, undefined);
    };
  }, [editor, catalog]);

  const onEditorDidMount = useCallback((ed: monacoTypes.editor.IStandaloneCodeEditor, monaco: Monaco) => {
    // An action belongs to this editor; addCommand would bind Shift+Enter for every editor on the
    // page to the last one mounted.
    ed.addAction({
      id: 'sls.runQuery',
      label: 'Run query',
      keybindings: [monaco.KeyMod.Shift | monaco.KeyCode.Enter],
      run: () => {
        latest.current.onChange(ed.getValue());
        latest.current.onRunQuery?.();
      },
    });
    setEditor({ ed, monaco });
  }, []);

  return (
    <CodeEditor
      value={value}
      language={slsLanguageDefinition.id}
      height={height}
      showLineNumbers={false}
      showMiniMap={false}
      onBlur={onChange}
      onBeforeEditorMount={registerLanguage}
      onEditorDidMount={onEditorDidMount}
      monacoOptions={{
        wordWrap: 'on',
        scrollBeyondLastLine: false,
        renderLineHighlight: 'none',
        fixedOverflowWidgets: true,
        suggestFontSize: 12,
        fontSize: 13,
      }}
    />
  );
}
