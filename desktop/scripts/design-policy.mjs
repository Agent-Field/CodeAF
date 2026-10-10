import ts from 'typescript';
export function inspectSource(path, source) {
 const violations = [];
 const ast = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
 const add = (node, message) => violations.push(`${path}:${ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1}: ${message}`);
 const primitives = path.startsWith('src/components/ui/');
 const vendorBoundary = path === 'src/components/ui/Icon.tsx';
 const approvedRadix = { 'src/components/ui/Select.tsx': ['@radix-ui/react-select'], 'src/components/ui/Menu.tsx': ['@radix-ui/react-context-menu','@radix-ui/react-dropdown-menu'], 'src/components/ui/HoverPreview.tsx': ['@radix-ui/react-hover-card'] };
 const graphBoundary = path === 'src/components/ui/DependencyMap.tsx';
 const brandBoundary = path === 'src/components/ui/BrandMark.tsx';
 const modalBoundary = path === 'src/components/CommandPalette.tsx';
 function visit(node) {
  if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
   const module = node.moduleSpecifier.text;
   if (module.startsWith('@radix-ui/') && !approvedRadix[path]?.includes(module)) add(node, 'Use the approved shared themed control boundary; no per-screen Radix imports.');
   if (/(?:animateicons|lucide|heroicons|tabler|phosphor|hugeicons|react-icons)/i.test(module) && !vendorBoundary) add(node, 'Import icons through the shared Icon component only.');
   if (module.includes('@animateicons/react/huge')) add(node, 'Lucide is the approved icon family; do not mix families.');
  }
  if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
   const tag = node.tagName.getText(ast);
   if (tag === 'select') add(node, 'Use shared themed Select instead of an OS-styled popup.');
   if (['svg','path','circle','rect','polygon','line','polyline'].includes(tag) && !brandBoundary && !(graphBoundary && ['svg','path'].includes(tag))) add(node, 'Use Icon or BrandMark; no per-screen SVG artwork.');
   if (['button','input','select','textarea','h1','h2','code','kbd'].includes(tag) && !primitives && !modalBoundary) add(node, `Use shared UI primitives instead of raw <${tag}>.`);
   for (const attr of node.attributes.properties) {
    if (ts.isJsxAttribute(attr) && attr.name.getText(ast) === 'style') add(attr, 'Use token-backed CSS classes instead of inline style values.');
   }
  }
  ts.forEachChild(node, visit);
 }
 visit(ast);
 return violations;
}
export function inspectCss(path, source, design) {
 const violations = [];
 let css = source.replace(/\/\*[\s\S]*?\*\//g, '');
 css = css.replace(/@media\s*\(max-width:\s*(\d+)px\)/g, (rule, width) => {
  if (!Object.values(design.breakpoints).includes(Number(width))) violations.push(`${path}: breakpoint ${width}px is not in design/tokens.json.`);
  return rule.replace(`${width}px`, 'TOKEN');
 });
 const literals = css.match(/#[\da-f]{3,8}\b|(?:\d*\.)?\d+(?:px|rem|em|ms|s|deg)\b|(?:rgba?|hsla?|oklch)\([^)]*\)/gi) ?? [];
 if (literals.length) violations.push(`${path}: put raw design values in tokens.json: ${[...new Set(literals)].join(', ')}`);
 const tokens = new Set([...Object.keys(design.foundation), ...Object.keys(design.themes.light), ...Object.keys(design.themes.dark)]);
 for (const match of css.matchAll(/var\(--([\w-]+)/g)) if (!tokens.has(match[1]) && !['radix-select-content-available-height','radix-select-content-transform-origin','radix-context-menu-content-available-height','radix-context-menu-content-available-width','radix-context-menu-content-transform-origin','radix-dropdown-menu-content-available-height','radix-dropdown-menu-content-transform-origin','radix-hover-card-content-transform-origin'].includes(match[1])) violations.push(`${path}: unknown token --${match[1]}.`);
 for (const match of css.matchAll(/(?:font-size|font-weight|font-family|line-height|letter-spacing|opacity|stroke-width)\s*:\s*([^;}]+)/g)) {
  if (!/^(?:var\(--[\w-]+\)|inherit|normal)$/.test(match[1].trim())) violations.push(`${path}: typography and opacity must use tokens: ${match[0]}`);
 }
 for (const match of css.matchAll(/(?:^|[;{])\s*(?:color|background-color|border-color|fill|stroke)\s*:\s*([^;}]+)/g)) {
  if (!/^(?:var\(--[\w-]+\)|inherit|currentColor|transparent|none)$/.test(match[1].trim())) violations.push(`${path}: colors must use semantic tokens: ${match[0]}`);
 }
 for (const match of css.matchAll(/--[\w-]+\s*:/g)) violations.push(`${path}: define tokens only in design/tokens.json.`);
 return violations;
}
