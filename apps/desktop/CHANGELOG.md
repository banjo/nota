# @nota/desktop

## 0.1.2

### Patch Changes

- [#25](https://github.com/banjo/nota/pull/25) [`020bccc`](https://github.com/banjo/nota/commit/020bccc9c451eb84f9714bb2018c92d22ad762a8) - Clicking in a note in Reading mode, or in a preview of an outside file, starts editing where you clicked.

- [#27](https://github.com/banjo/nota/pull/27) [`9dbabe5`](https://github.com/banjo/nota/commit/9dbabe5ab8c77fb3efefce79c929e883a85b9f28) - Code blocks are coloured in their language, in every theme. Themes can set the colours with the `md-syntax-*` tokens.

- [#24](https://github.com/banjo/nota/pull/24) [`4d1dc47`](https://github.com/banjo/nota/commit/4d1dc47d62bfd2b8460c405cea51cbfb0ffa2bec) - Tables render as tables in Live and Reading mode, and show their source while the cursor is in one. Themes can colour them with `md-table-border` and `md-table-header`.

- [#26](https://github.com/banjo/nota/pull/26) [`2b0f492`](https://github.com/banjo/nota/commit/2b0f492a26d1e814ef24c9cfcc45f7490a892d33) - Settings follow you: while signed in, the theme, colour theme, note mode and properties sync with your account, and `~/.config/nota/settings.toml` sets them from your dotfiles. `nota settings` shows where each value comes from.

- [#22](https://github.com/banjo/nota/pull/22) [`f827dd5`](https://github.com/banjo/nota/commit/f827dd50b8d4701fa41737c92360baa85fa8bd3e) - Themes: nine classics (Dracula, Nord, Solarized, Gruvbox, Catppuccin, Tokyo Night, Rosé Pine, One, GitHub), your own themes from `.nota/plugins/`, and `nota plugins` to check them.
