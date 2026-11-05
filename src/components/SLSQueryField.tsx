import { CodeEditor, type Monaco, type monacoTypes } from '@grafana/ui';
import React, { useCallback, useEffect, useRef, useState } from 'react';

import { getCompletionProvider, setModelFields } from '../language/completion';
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
  /** Field names suggested while typing. */
  fields?: string[];
}

/** SLS search / SPL / SQL editor with syntax highlighting and completion. */
export function SLSQueryField({ value, onChange, onRunQuery, height = 80, fields }: Props) {
  // The Shift+Enter command is registered once on mount; read the latest callbacks through refs.
  const latest = useRef({ onChange, onRunQuery });
  useEffect(() => {
    latest.current = { onChange, onRunQuery };
  }, [onChange, onRunQuery]);

  const [modelUri, setModelUri] = useState<string>();
  useEffect(() => {
    if (!modelUri) {
      return;
    }
    setModelFields(modelUri, fields);
    return () => setModelFields(modelUri, undefined);
  }, [modelUri, fields]);

  const onEditorDidMount = useCallback((editor: monacoTypes.editor.IStandaloneCodeEditor, monaco: Monaco) => {
    editor.addCommand(monaco.KeyMod.Shift | monaco.KeyCode.Enter, () => {
      latest.current.onChange(editor.getValue());
      latest.current.onRunQuery?.();
    });
    setModelUri(editor.getModel()?.uri.toString());
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
