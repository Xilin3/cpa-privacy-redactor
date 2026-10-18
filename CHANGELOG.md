# Changelog

## 0.1.0 - 2026-10-18

- Initial source release of the Claude request privacy normalizer.
- Redact known location and machine fields from generated environment context and metadata.
- Remove optional location hints from native WebSearch declarations.
- Preserve task content, tool arguments, signatures, and identity protocol structure.
- Include the previously validated Linux ARM64 shared library as a release asset.

The plugin processes selected request body fields. It does not anonymize account identifiers, HTTP headers, network addresses, or arbitrary task content.
