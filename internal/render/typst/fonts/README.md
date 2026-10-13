# Embedded print fonts

This records the source and license of each embedded print font. Update it
when you add or replace a font. [Font declarations](../fonts_embed.go) list the
files the application embeds.

The additional Unicode fallback fonts use the SIL Open Font License:

| Font | Source | SHA-256 |
| --- | --- | --- |
| `NotoEmoji.ttf` | [Noto Emoji](https://github.com/google/fonts/blob/23e54b51ddffbc7713c583748e3bd86f62b1fa4a/ofl/notoemoji/NotoEmoji%5Bwght%5D.ttf) | `de6c18832938afc99caf132b39d6a30a19bac7f2e812e28db2535b4608d27551` |
| `NotoSansSymbols2-Regular.ttf` | [Noto Sans Symbols 2](https://github.com/google/fonts/blob/23e54b51ddffbc7713c583748e3bd86f62b1fa4a/ofl/notosanssymbols2/NotoSansSymbols2-Regular.ttf) | `7d5fb73b7ca67a6798101741f5d280a3d016a56a197afcd4199dbb57b4b82a21` |

Noto Emoji is the upstream variable monochrome font, renamed without changing
its bytes. Its weight axis supports regular and semibold text on paper.
