# arca — visual identity

## Files

| File | Use |
|---|---|
| `arca-logo-dark.svg` / `.png` | Horizontal lockup, transparent background, light letters — README in dark theme |
| `arca-logo-light.svg` / `.png` | Horizontal lockup, transparent background, dark letters — README in light theme, site, docs |
| `arca-banner.svg` / `.png` | Lockup on a rounded dark plate — top of the README, slides |
| `arca-icon.svg`, `arca-icon-512.png` | The symbol alone, transparent |
| `arca-icon-badge.svg`, `arca-icon-badge-512.png`, `-256.png` | Symbol on a rounded plate — org avatar, app icon, Homebrew |
| `favicon-64.png`, `favicon-32.png` | Favicon for the docs |
| `arca-social.svg`, `arca-social-1280x640.png` | GitHub social preview (Settings → Social preview) |
| `arca-ascii.txt` | Every ASCII variant of the CLI banner |

No SVG depends on an installed font: the "arca" wordmark is drawn as paths, so
it renders identically in any browser, on GitHub, and in any converter.

## Palette

| Role | Hex | ANSI truecolor |
|---|---|---|
| Hull, cursor, prompt | `#56D364` | `38;2;86;211;100` |
| Roof, cabin frame | `#3FB950` | `38;2;63;185;80` |
| Water | `#58A6FF` | `38;2;88;166;255` |
| Lock (encryption) | `#E3B341` | `38;2;227;179;65` |
| Light text | `#E6EDF3` | `38;2;230;237;243` |
| Terminal background | `#0D1117` | — |
| Surface | `#161B22` | — |

This is the GitHub Dark palette, which in turn follows the classic terminal
colors (green for success, blue for information, amber for attention). On
16-color terminals, degrade to `92` / `32` / `94` / `93` / `97`.

## Typography

The wordmark is drawn, not typeset. For text that accompanies the brand (docs,
site, `--help`), use a wide-grid monospace: **JetBrains Mono**, **IBM Plex
Mono** or **Iosevka**. All are openly licensed and have a single-storey
lowercase `a`, which matches the wordmark.

## README snippet

Automatic light/dark switching on GitHub:

```html
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/arca-logo-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/arca-logo-light.svg">
    <img alt="arca" src="docs/assets/arca-logo-light.svg" width="360">
  </picture>
</p>

<p align="center">
  Single-file encrypted backups for Unix-like systems. One binary, no runtime
  dependencies.
</p>
```

## Usage rules

- Clear space around the mark: the height of the cursor block (the green square).
- Minimum lockup size: 120 px wide. Below that, use the symbol alone.
- Do not rotate the ark, do not change the green of the hull, and do not place
  the transparent-background version over photos — use `arca-banner.svg`.
