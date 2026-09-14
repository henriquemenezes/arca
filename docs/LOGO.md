# arca — identidade visual

## Arquivos

| Arquivo | Uso |
|---|---|
| `arca-logo-dark.svg` / `.png` | Lockup horizontal, fundo transparente, letras claras — README em tema escuro |
| `arca-logo-light.svg` / `.png` | Lockup horizontal, fundo transparente, letras escuras — README em tema claro, site, docs |
| `arca-banner.svg` / `.png` | Lockup sobre placa escura arredondada — topo do README, slides |
| `arca-icon.svg`, `arca-icon-512.png` | Só o símbolo, transparente |
| `arca-icon-badge.svg`, `arca-icon-badge-512.png`, `-256.png` | Símbolo em placa arredondada — avatar da org, ícone de app, Homebrew |
| `favicon-64.png`, `favicon-32.png` | Favicon das docs |
| `arca-social.svg`, `arca-social-1280x640.png` | Social preview do GitHub (Settings → Social preview) |
| `arca-ascii.txt` | Todas as variantes ASCII do banner do CLI |

Nenhum SVG depende de fonte instalada: o logotipo "arca" é desenhado em
paths, então renderiza idêntico em qualquer navegador, no GitHub e em
qualquer conversor.

## Paleta

| Papel | Hex | ANSI truecolor |
|---|---|---|
| Casco, cursor, prompt | `#56D364` | `38;2;86;211;100` |
| Telhado, moldura da cabine | `#3FB950` | `38;2;63;185;80` |
| Água | `#58A6FF` | `38;2;88;166;255` |
| Fechadura (criptografia) | `#E3B341` | `38;2;227;179;65` |
| Texto claro | `#E6EDF3` | `38;2;230;237;243` |
| Fundo terminal | `#0D1117` | — |
| Superfície | `#161B22` | — |

É a paleta do GitHub Dark, que por sua vez segue as cores clássicas de
terminal (verde de sucesso, azul de informação, âmbar de atenção). Em
terminais de 16 cores, degrade para `92` / `32` / `94` / `93` / `97`.

## Tipografia

O logotipo é desenhado, não composto. Para textos que acompanham a marca
(docs, site, `--help`), use uma monoespaçada de grade larga:
**JetBrains Mono**, **IBM Plex Mono** ou **Iosevka**. Todas têm licença
aberta e um `a` de caixa-baixa de um andar, que combina com o logotipo.

## Snippet do README

Troca automática entre tema claro e escuro no GitHub:

```html
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/arca-logo-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/arca-logo-light.svg">
    <img alt="arca" src="docs/assets/arca-logo-light.svg" width="360">
  </picture>
</p>

<p align="center">
  backup cifrado e comprimido para Linux e macOS
</p>
```

## Regras de uso

- Espaço livre em volta da marca: a altura do bloco do cursor (o quadrado verde).
- Tamanho mínimo do lockup: 120 px de largura. Abaixo disso, use só o símbolo.
- Não gire a arca, não troque o verde do casco e não coloque a versão de
  fundo transparente sobre fotos — use `arca-banner.svg`.
