# Build charts from upstream modules

ApexCharts 4.7.0's prebundled entry produced a 581.11 kB minified chunk,
above Vite's unchanged 500 kB warning limit. Lazy loading that single file
did not address its size.

Resolve the same pinned release's source entry instead. Rollup can then
separate the SVG dependency (94.70 kB) from the chart engine and renderers
(under 500 kB). Renderers and the engine import each other and must stay
in the same chunk; splitting them creates a circular chunk warning. Import the upstream CSS as an inline string,
matching the library's own style injection. No chart types, interactions,
styles, or application controls are removed, and no warning limit changes.

Validation: the production build finishes without chunk warnings; all 66
existing frontend tests pass. A disposable authenticated binary captured
63 page, tab, detail, and form states at desktop, mobile, and 4K sizes.
Every control inventory matches, and all three Dashboard screenshots are
pixel-identical to the pre-change binary using the same database. The
other screenshot differences are transient hover/focus/caret states;
their rendered text and control inventories match exactly.
