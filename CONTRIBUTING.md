# Contributing

This project follows [@jaredallard's contribution
guidelines](https://github.com/jaredallard/jaredallard/blob/master/CONTRIBUTING.md).

<!-- <<Stencil::Block(custom)>> -->

## Building the image

The image includes `asbx-op` and `asbx-clip`, built for its platform by goreleaser, so
it can't be built with `docker build` on its own. Build it with:

```bash
goreleaser release --snapshot --clean
```

<!-- <</Stencil::Block>> -->
