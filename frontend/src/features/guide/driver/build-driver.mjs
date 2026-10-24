// Driver.js 1.4.0; private cleanup.1, host-pointer.1 and prefix.1 adaptation.
import { readFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { fileURLToPath, URL as NodeURL } from 'node:url'
import { resolve } from 'node:path'

export const ORIGINAL_JS_SHA256 = 'fa9a75e1abcb7834f117c9f6d24cd43ccfde0cb7767753c1092b8561770bdf84'
export const ORIGINAL_CSS_SHA256 = '4b398481a7ce8375af4d9f58f39410c73a0b70726fe513686d3d51c10ad76cb5'
const folder = fileURLToPath(new NodeURL('.', import.meta.url))
const dependency = fileURLToPath(new NodeURL('../../../../node_modules/driver.js/', import.meta.url))
const hash = value => createHash('sha256').update(value).digest('hex')
const patches = [
  [
    "document.addEventListener(",
    "__s2oListen(document, ",
    5
  ],
  [
    "__s2oListen(document, \n",
    "__s2oListen(document,\n",
    1
  ],
  [
    "window.addEventListener(",
    "__s2oListen(window, ",
    4
  ],
  [
    "window.requestAnimationFrame(",
    "__s2oFrame(",
    3
  ],
  [
    "window.cancelAnimationFrame(",
    "__s2oCancelFrame(",
    1
  ],
  [
    "function _e() {\n",
    "function _e() {\n  __s2oRelease();\n",
    1
  ],
  [
    "function be(e, o) {\n",
    "function be(e, o) {\n  __s2oRemember(e);\n",
    1
  ],
  [
    "n.removeAttribute(\"aria-controls\"), ((C",
    "n.removeAttribute(\"aria-controls\"), __s2oRestorePrevious(n, e), ((C",
    1
  ],
  [
    "function Ce() {\n  var e;\n  (e = document.getElementById(\"driver-dummy-element\")) == null || e.remove(), document.querySelectorAll(\".driver-active-element\").forEach((o) => {\n    o.classList.remove(\"driver-active-element\", \"driver-no-interaction\"), o.removeAttribute(\"aria-haspopup\"), o.removeAttribute(\"aria-expanded\"), o.removeAttribute(\"aria-controls\");\n  });\n}",
    "function Ce() {\n  __s2oRestoreAll();\n}",
    1
  ],
  [
    "\"driver-dummy-element\"",
    "\"s2o-driver-dummy-element\"",
    8
  ],
  [
    "\"driver-active\"",
    "\"s2o-driver-active\"",
    2
  ],
  [
    "  const u = !f && v;",
    "  if (!l(\"isInitialized\")) return;\n  const u = !f && v;",
    1
  ],
  [
    "}), k(\"__transitionCallback\", void 0), k(\"__previousStep\", d), k(\"__previousElement\", n), k(\"__activeStep\", o), k(\"__activeElement\", e)), __s2oFrame(m);",
    "}), l(\"isInitialized\") && (k(\"__transitionCallback\", void 0), k(\"__previousStep\", d), k(\"__previousElement\", n), k(\"__activeStep\", o), k(\"__activeElement\", e))), l(\"isInitialized\") && __s2oFrame(m);",
    1
  ],
  [
    "t && document.body.removeChild(t.wrapper)",
    "t && t.wrapper.remove()",
    1
  ]
]

function replaceExact(source, before, after, count = 1) {
  if (source.split(before).length - 1 !== count) throw new Error('Driver patch context changed; generation refused')
  return source.split(before).join(after)
}
function prefix(source, count) {
  const pattern = /(["'`.#\s])driver-/g
  if ([...source.matchAll(pattern)].length !== count) throw new Error('Driver prefix context changed; generation refused')
  const output = source.replace(pattern, '$1s2o-driver-')
  if (/(["'`.#\s])driver-/.test(output)) throw new Error('Unprefixed Driver class remains')
  return output
}
export function generateDriver() {
  let js = readFileSync(resolve(dependency, 'dist/driver.js.mjs'), 'utf8')
  let css = readFileSync(resolve(dependency, 'dist/driver.css'), 'utf8')
  if (hash(js) !== ORIGINAL_JS_SHA256 || hash(css) !== ORIGINAL_CSS_SHA256) throw new Error('Unknown Driver source; generation refused')
  for (const [before, after, count] of patches) js = replaceExact(js, before, after, count)
  const prelude = readFileSync(resolve(folder, 'cleanup-prelude.js'), 'utf8')
  js = '/* eslint-disable */\n// Driver.js 1.4.0 + cleanup.1 + prefix.1; MIT, see LICENSE\n' + prefix(prelude + '\n' + js, 44)
  css = replaceExact(css, '.driver-active .driver-overlay,.driver-active *{pointer-events:none}', '.driver-active .driver-overlay{pointer-events:none}')
  css = replaceExact(css, '.driver-active ', '.s2o-driver-active ', 3)
  css = prefix(css, 62)
  return { js, css }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const { js, css } = generateDriver()
  writeFileSync(resolve(folder, 'driver.mjs'), js)
  writeFileSync(resolve(folder, 'driver.css'), css)
}
