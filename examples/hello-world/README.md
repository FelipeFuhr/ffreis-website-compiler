# Hello World Example

Build:

```bash
website-compiler build \
  -website-root ffreis-website-compiler/examples/hello-world \
  -out ffreis-website-compiler/examples/hello-world/dist \
  -run-output-tests
```

Then open:
- `ffreis-website-compiler/examples/hello-world/dist/index.html`

Expected source layout under `-website-root`:
- `src/templates`
- `src/assets`
- `tests/output.yaml` (optional website-owned assertions for compiled output)

The bundled `tests/output.yaml` demonstrates a small, stable smoke test. It
asserts public output only; it does not run browser automation, execute shell
commands, or encode compiler-specific product behavior.

To run the same output assertions without rebuilding:

```bash
website-compiler test-output \
  -website-root ffreis-website-compiler/examples/hello-world \
  -out ffreis-website-compiler/examples/hello-world/dist
```
