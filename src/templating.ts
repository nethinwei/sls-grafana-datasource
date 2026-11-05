import type { ScopedVars, TypedVariableModel } from '@grafana/data';
import type { TemplateSrv } from '@grafana/runtime';

/**
 * `key: $var` / `key:${var}` in a search statement. When `var` is multi-valued it expands to
 * `(key: "a" or key: "b")`, the only multi-value form SLS search accepts (`key: (a or b)` is a
 * syntax error).
 */
const FIELD_VAR = /([\w.@:-]+?)\s*:\s*\$(?:\{(\w+)(?::\w+)?\}|(\w+))/g;

type VarLike = Pick<TypedVariableModel, 'name'> & {
  multi?: boolean;
  includeAll?: boolean;
  label?: string;
  description?: string | null;
};

function isMultiValued(v: VarLike | undefined): boolean {
  return Boolean(v && (v.multi || v.includeAll));
}

function quote(value: string): string {
  return '"' + value.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"';
}

/** Values of a variable after scoping, as an array (single values become a one-element array). */
function variableValues(name: string, scopedVars: ScopedVars | undefined, templateSrv: TemplateSrv): string[] {
  const json = templateSrv.replace('${' + name + ':json}', scopedVars);
  try {
    const parsed = JSON.parse(json);
    return Array.isArray(parsed) ? parsed.map(String) : [String(parsed)];
  } catch {
    return [json];
  }
}

/**
 * Formatter for variables that are not used as `key: $var`. Kept compatible with plugin 2.x:
 * multi values are joined with " OR ", and variables whose description contains `field_search`
 * (or whose name equals their label) render as `"name":"value"` pairs.
 */
export function legacyMultiValueFormat(value: unknown, variable: VarLike): string {
  if (Array.isArray(value)) {
    const fieldSearch = variable.name === variable.label || Boolean(variable.description?.includes('field_search'));
    const parts = value.map((v) => (fieldSearch ? `"${variable.name}":"${v}"` : String(v)));
    return parts.join(' OR ');
  }
  return String(value ?? '');
}

/** Replaces template variables in an SLS query. */
export function interpolateQuery(query: string, scopedVars: ScopedVars | undefined, templateSrv: TemplateSrv): string {
  const variables = new Map<string, VarLike>(templateSrv.getVariables().map((v) => [v.name, v as VarLike]));

  const expanded = query.replace(FIELD_VAR, (match: string, key: string, braced?: string, bare?: string) => {
    const name = braced ?? bare ?? '';
    if (!isMultiValued(variables.get(name)) || (scopedVars && scopedVars[name])) {
      return match;
    }
    const values = variableValues(name, scopedVars, templateSrv);
    if (values.length === 1) {
      return values[0] === '*' ? `${key}: *` : `${key}: ${quote(values[0])}`;
    }
    return '(' + values.map((v) => `${key}: ${quote(v)}`).join(' or ') + ')';
  });

  return templateSrv.replace(expanded, scopedVars, (value: unknown, variable: VarLike) =>
    legacyMultiValueFormat(value, variable)
  );
}

/** Plugin 2.x stored logstores picked from a variable as `__custom__@name`. */
export function interpolateLogstore(
  logstore: string | undefined,
  scopedVars: ScopedVars | undefined,
  templateSrv: TemplateSrv
): string {
  if (!logstore) {
    return '';
  }
  const legacy = /^__custom__@(.+)$/.exec(logstore);
  return templateSrv.replace(legacy ? '$' + legacy[1] : logstore, scopedVars);
}
