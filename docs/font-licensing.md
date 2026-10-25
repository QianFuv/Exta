# Font licensing evidence

Exta retains JetBrainsLxgwNerdMono v1.3, its 344 Regular and 324 Bold WOFF2 files,
both local font CSS files, font family names, weights, glyphs and Unicode ranges.
The licensing correction changes provenance and notice distribution only. It does
not rely on a claim that web delivery alone exempts a merged font from naming rules.

## Identified source chain

1. The official [merged-font v1.3 split archive](https://github.com/lvbibir/JetBrainsLxgwNerdMono/releases/download/v1.3/JetBrainsLxgwNerdMono-split-woff2.zip)
   has SHA-256 `535c724568f24cad90bde45765fcc98ab623246fdf4f39ba3e9d1391ed7d2f2a`.
   Every local WOFF2 payload matches a member of that archive. Local filenames
   changed; the local CSS also differs and is not claimed byte-identical to the
   upstream CSS.
2. The release's source commit is
   `a1d8b32ecf0c0fcffcf5df01ada5c2e135754799`. Its [configuration](https://raw.githubusercontent.com/lvbibir/JetBrainsLxgwNerdMono/a1d8b32ecf0c0fcffcf5df01ada5c2e135754799/config.yaml)
   selects `fonts/LXGWZhenKaiGB-Regular.ttf` for Bold. That vendored source font
   has Git blob `e70f359b74d9d653881fa04304885671775799f1` and SHA-256
   `b413151b90f3c96c0d8905eb55c7078f0e3aca9cfb0d962f3826ab185f2e6439`.
3. That hash equals the official [ZhenKai v0.819 release asset](https://github.com/lxgw/LxgwZhenKai/releases/tag/v0.819).
   Its actual name table identifies Version 0.819, December 5, 2025, declares
   OFL-1.1, and retains LXGW and Klee copyright notices. The merged-font release
   was published January 6, 2026.
4. The [license at the ZhenKai v0.819 source commit](https://raw.githubusercontent.com/lxgw/LxgwZhenKai/f98cd0b197e80211e7f77e9171d29d0659b430bf/OFL.txt)
   declares no RFNs in its copyright header. Its SHA256-UTF8-LF-v1 digest is
   `02c29dec8b0f48d1c73ed28e16824dcf9c1c6c3c2870d31e2a556a880b8ade7a`.
   This is the text distributed as `LXGWZhenKai-OFL.txt`.

The newer ZhenKai OFL introduces RFNs and additional permissions. It was collected
previously without identifying the source version used in the merged font. The
historical source and license above provide the version-specific basis for keeping
the current names. This does not claim any blanket permission under the newer
webfont exception, infer permission from letter case, or remove an RFN declaration
from a newer source version. See the official [OFL FAQ](https://openfontlicense.org/ofl-faq/),
especially sections 4.6 and 5.7, for the license author's explanation of prior grants
and explicit name reservations.

## Reproducible verification

`web/src/assets/fonts/provenance.json` records immutable source URLs, the source
and archive hashes, all 668 local-to-upstream WOFF2 member mappings, exact local
WOFF2/CSS hashes and exact distributed notice hashes. Download the named official
archive, verify its SHA-256, then compare each mapped ZIP member's SHA-256 with the
corresponding local file entry. The CSS entries describe only the preserved local
files. Download the pinned vendored ZhenKai source, verify its SHA-256 and Git blob,
and compare it with the official v0.819 asset. Decode the pinned OFL as UTF-8,
remove at most one leading BOM, normalize CRLF/CR to LF without trimming, and hash
the UTF-8 bytes to reproduce the historical license digest.

The local build rejects changes to the recorded font, CSS or notice set before
promoting a production bundle. Existing source/provenance/license checks also reject
an archive built from stale inputs. Hash equality establishes the recorded byte
correspondence; the record is not an independent legal warranty or a claim to have
audited every third-party font project.

## Distribution

Font data is described as OFL-1.1. Nerd Fonts' original licensing text is retained;
its MIT tooling notice does not relabel the patched font data as MIT. The supplied
patched JetBrains font's own OFL text is included explicitly. Actual component
copyright notices are retained in the accompanying NOTICE.

A static `rel=license` link on the production page points to a same-origin
`font-licenses.txt` asset with the full notice, license texts and source record.
The asset is covered by the same manifest, checksums and immutable namespace as the
font shards. Native archives and the container retain the same font notices and
source record in their frontend license evidence. No CSP permission or desktop
font distribution is added.
