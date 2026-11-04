# App icon

Tailmulti's icon is an Icon Composer file, `macos/Resources/AppIcon.icon`. The build
compiles it with `actool` into `Assets.car` (Liquid Glass) plus a fallback
`AppIcon.icns`. Editable source layers live in `macos/Design/AppIcon/`.

## Apple's rules that matter here

From [App icons (HIG)](https://developer.apple.com/design/human-interface-guidelines/app-icons)
and [Creating your app icon using Icon Composer](https://developer.apple.com/documentation/xcode/creating-your-app-icon-using-icon-composer):

- **Canvas:** 1024 × 1024 px for macOS. Layers are square and unmasked; "the system
  masks all layer edges to produce an icon's final shape." Don't export the mask.
- **Background:** "Remove background colors and gradients" before export, and set the
  background fill in Icon Composer. "Avoid using black for your icon's background."
- **Effects:** "Remove blurs and shadows, and specular, opacity, and translucency
  settings" from exported layers; set them in Icon Composer instead.
- **Layers:** separate colors into layers you may want to vary, export them fully opaque,
  and "give the layers meaningful names that include numbers (increment from back to
  front)." Dragging a folder into Icon Composer creates a group; use at most four groups.
- **Format:** prefer SVG. "Prefer clearly defined edges in foreground layers"; avoid very
  thin lines and fine detail that disappear at small sizes.
- **Appearances:** design for default, dark and mono (clear and tinted), keeping the same
  core features in each.
- **Templates:** grids and production templates are at
  [Apple Design Resources](https://developer.apple.com/design/resources/).

## Repository constraints

- Keep the file name `AppIcon.icon` at `macos/Resources/`. The build passes
  `--app-icon AppIcon`, and `CFBundleIconName`/`CFBundleIconFile` are `AppIcon`.
- **Use SVG layers only.** The source audit rejects binary files, so PNG layers would
  fail CI. Convert text to outlines.
- The build fails on any `actool` warning.

## Workflow

1. Open Icon Composer (Xcode > Open Developer Tool > Icon Composer). Either open
   `macos/Resources/AppIcon.icon`, or start a new file and drag in
   `macos/Design/AppIcon/1 Grid` and `macos/Design/AppIcon/2 Accounts`. Each folder
   becomes a group and each SVG a layer.
2. Set the icon's background fill (select the icon name in the sidebar, Style inspector >
   Fill). Choose a dark gradient that isn't pure black.
3. Adjust per group: opacity for the grid dots; Liquid Glass mode, specular, translucency
   and shadow for the account dots. Specular on small dots draws a ring around each one,
   so check it at full size.
4. Preview Default, Dark and Mono (clear and tinted) for macOS at 100% zoom and the small
   preview sizes. Zooming past 100% enlarges a bitmap and always looks soft.
5. Save as `macos/Resources/AppIcon.icon`, replacing the existing file. If you edited the
   SVGs, keep the copies in `macos/Design/AppIcon/` in sync.
6. Build and check:

   ```sh
   ./scripts/build-app.sh --candidate
   open -R "$(cat build/candidate-app-path.txt)"
   ```

   Finder shows the compiled icon. `scripts/verify-app.py` (run by the build) checks that
   `Assets.car` and `AppIcon.icns` are bundled.

## Current source layers

| File | Content | Fill |
| --- | --- | --- |
| `1 Grid/1-grid.svg` | the eight remaining dots of the 4×4 grid | `#8E93A3`, opaque |
| `2 Accounts/2-account-blue.svg` | top-left 2×2, first account | `#4C8DFF` |
| `2 Accounts/3-account-teal.svg` | bottom-right 2×2, second account | `#2ED3B7` |

The grid has a 186 px pitch and 57 px dot radius, centred on the canvas (dot centres at
233, 419, 605 and 791). The menu bar icon in `StatusMenu.swift` draws the same grid in
monochrome; keep the two consistent if the layout changes.
