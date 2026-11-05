# ADR-0014: Predownload is an accelerator, never the authority

- Status: Accepted
- Date: 2026-11-05
- Relates to: FR-036–FR-041, AC-030–AC-033

## Context

nala downloads archives in parallel before dpkg runs. xbps also downloads, but
serially inside the transaction. Reimplementing xbps's resolution or
verification would fork the contract; doing nothing loses the feature. The
correct split is to fill the cache in parallel and leave every decision to
xbps.

## Decision

1. **Scope** — every real transaction with a download set predownloads after
   the prompt/root checks and before the child. `--dry-run` and
   `--print-uris` download nothing; `download` and `--download-only` use the
   same engine but have no fallback.
2. **URLs** — only plan-derived `<repo>/<pkgver>.<arch>.xbps[.sig2]`. No
   invented mirror, suffix or filename. Local/`file://` rows are excluded
   because xbps never caches local archives; `download` copies them.
3. **Hash** — SHA-256 from the bulk `filename-sha256` pass, keyed by
   `(repository, pkgver)`. A missing hash is a warning for the cache (xbps
   verifies later) and a refusal for cwd output.
4. **Transport** — Go stdlib `net/http` only, three transfers per host, one
   task per archive, four attempts per URL with no backoff, 404 and other 4xx
   non-retryable, `.part` + fsync + atomic rename, no resume, environment
   proxy, system TLS roots, https→http redirects refused.
5. **Trust** — RSA `.sig2` verification is deferred to xbps. xbpretty's
   guarantee for `download` is SHA-256 from authenticated repository metadata,
   not "the signature was verified here".
6. **Failure** — a real transaction warns `predownload: N of M ...; xbps will
   download them` and continues with xbps's exit code. Explicit downloads
   abort the run on the first exhausted file or hash mismatch, keeping files
   already verified.
7. **Placement correction** — `--download-only` stops after the prompt/root/
   non-TTY checks, not before them (#43's ordered list had a drafting slip;
   its "same rules as a real transaction" governs).

## Consequences

- A broken archive fetch never changes a transaction's outcome.
- `download` and `--download-only` are honest: they fail when the cache cannot
  be filled, because no child will take over.
- `-c`/`-r` passthrough disables predownload rather than writing to a cache
  xbpretty did not model. No config key is introduced.
