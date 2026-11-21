# Deadman Danny: Meshy prompts

Replace `{{CONCEPT}}` with a description of your character, for example *Rook, a hulking stone golem with glowing blue runes and a lantern for a heart*, and `{{CHARACTER}}` with its short name (*Rook*). Or set `concept` and `select.name` in `workspace.json` and run `dlmod meshy deadpack` again to fill them in.

Everything: about 206 credits (156 without the optional steps). Prices are Meshy's as of 2026-10; confirm before generating.

## 1. Character design sheet

- Tool: `meshy_text_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 16:9
- Save as: `assets/design_sheet.png`
- Every later picture uses this sheet as its reference, so redo it until the character is right.

```text
Character design sheet for a 3D game character: {{CONCEPT}}. It replaces Deadman Danny in Deadlock, a stylized third-person hero shooter. Its abilities are Strix Salvo, Chimera Mk1, Jettison Fuel, Close The Deal. The character must read at a glance from behind in third person, so give it a strong silhouette and one or two bold colours. It carries its own take on Deadman Danny's weapon, Chimera Peace Bringer. Show a large hero render, a front / three-quarter / side / back turnaround in a relaxed A-pose, close-ups of the head, hands and weapon, a colour palette with hex codes, and its two or three iconic symbols. Stylized PBR, clean studio light, dark background, labels only.
```

## 2. Front, side and back views for 3D

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Reference: `assets/design_sheet.png`
- Options: `generate_multi_view: true`
- Save as: `assets/views.png`
- Empty hands: a held weapon is better as its own model (weapon step), and dlmod can keep the hero's gun instead (-keep gun).

```text
{{CHARACTER}} from the reference sheet alone, full body, standing in a relaxed A-pose (arms 45 degrees down, legs slightly apart), empty hands, neutral expression, even light, plain light grey background, no shadows, no text.
```

## 3. 3D model

- Tool: `meshy_multi_image_to_3d`, model `meshy-7.1`, texture `meshy-7`, about 30 credits
- Reference: `assets/views.png`
- Options: `enable_pbr: false`, `texture_resolution: 2k`
- Save as: `assets/model.glb`, replaces `models/heroes_wip/deadpack/deadpack.vmdl`
- Detailed geometry reads best close up in third person, and Meshy models of about 200k triangles install fine. dlmod scales it to Deadman Danny's height (137 units) and rigs it to Deadman Danny's skeleton. For a lighter model use meshy_image_to_3d with meshy-t2.5 + meshy-n1 (15 credits, target_polycount 60000) on the front view.

## 4. Rig (humanoid characters) (optional)

- Tool: `meshy_rig`, about 5 credits
- Reference: `assets/model.glb`
- Save as: `assets/model.glb`
- Only for characters with arms and legs: dlmod maps Meshy's joints to the hero's bones by their structure. Skip it for blob-like characters: dlmod rigs them on the hero's own bones (see Animations in README.md). Meshy's animation clips are not used: the game plays the hero's own clips.

## 5. Signature weapon or held prop (optional)

- Tool: `meshy_text_to_3d`, model `meshy-t2.5`, about 15 credits
- Options: `refine: true`, `target_polycount: 15000`
- Save as: `assets/weapon.glb`
- Join it to model.glb in the right hand before installing, or keep the hero's own gun with -keep gun.

```text
A single game-ready prop: the signature weapon of {{CONCEPT}}. It is its own take on Deadman Danny's weapon, Chimera Peace Bringer. Stylized PBR in the character's colours, centered, no hands, no base.
```

## 6. Hero select screen art

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 16:9
- Reference: `assets/design_sheet.png`
- Save as: `assets/select.png`, replaces `panorama/images/heroes/backgrounds/deadpack_bg_psd.vtex_c` (4096x2312)
- dlmod select hides the hero's 3D scene so the picture shows on its own (scene=hide) and fades the left side to black under the hero grid.

```text
Splash art for a hero select screen: {{CHARACTER}} from the reference sheet in a setting that shows off its personality. Put the character in the RIGHT half of the frame, its centre at about 70% of the width and its head in the top third; keep the LEFT 45% calm and dark, it sits under the hero grid; keep the bottom-right corner simple, the name, tags and ability icons go there. Cinematic light, rich detail, no text, no logos.
```

## 7. Hero card

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 3:4
- Reference: `assets/design_sheet.png`
- Save as: `assets/card.png`, replaces `panorama/images/heroes/deadpack_card_psd.vtex_c` (280x380)
- dlmod apply keys out the green background.

```text
Hero card portrait of {{CHARACTER}} from the reference sheet: chest-up, three-quarter view, facing the viewer with confidence, its weapon in view, dramatic rim light, stylized 3D render, centered with some headroom. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 8. Card at low health

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 3:4
- Reference: `assets/design_sheet.png`
- Save as: `assets/card_critical.png`, replaces `panorama/images/heroes/deadpack_card_critical_psd.vtex_c` (280x380)
- dlmod apply keys out the green background.

```text
The same hero card portrait of {{CHARACTER}} from the reference sheet, badly hurt: scuffed and cracked, cringing but defiant, red danger light, same framing. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 9. Card after a kill

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 3:4
- Reference: `assets/design_sheet.png`
- Save as: `assets/card_gloat.png`, replaces `panorama/images/heroes/deadpack_card_gloat_psd.vtex_c` (280x380)
- dlmod apply keys out the green background.

```text
The same hero card portrait of {{CHARACTER}} from the reference sheet, triumphant: a taunting gesture after a kill, warm golden light, same framing. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 10. Tall portrait

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 9:16
- Reference: `assets/design_sheet.png`
- Save as: `assets/vertical.png`, replaces `panorama/images/heroes/deadpack_vertical_psd.vtex_c` (120x200)
- dlmod apply keys out the green background.

```text
Full-body portrait of {{CHARACTER}} from the reference sheet standing tall in its signature pose, the whole character in frame. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 11. Small portrait

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/sm.png`, replaces `panorama/images/heroes/deadpack_sm_psd.vtex_c` (128x128)
- dlmod apply keys out the green background.

```text
Head-and-shoulders portrait of {{CHARACTER}} from the reference sheet, front-facing, big simple shapes that read when small. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 12. Minimap icon

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/mm.png`, replaces `panorama/images/heroes/deadpack_mm_psd.vtex_c` (50x50)
- dlmod apply keys out the green background.

```text
Minimap icon of {{CHARACTER}} from the reference sheet: the head only, front view, flat colours, thick dark outline, readable at 32 pixels. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 13. Weapon picture

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 16:9
- Reference: `assets/design_sheet.png`
- Save as: `assets/gun.png`, replaces `panorama/images/heroes/guns/deadpack_gun_psd.vtex_c` (600x300)
- dlmod apply keys out the green background.

```text
Side view of the weapon or held prop of {{CHARACTER}} from the reference sheet, alone, horizontal, stylized 3D render. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 14. Ability icon: Strix Salvo

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_strix_salvo.png`, replaces `panorama/images/hud/abilities/deadpack/deadpack_salvo_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Strix Salvo", an ability of {{CHARACTER}} from the reference sheet: Launch a salvo of spirit missiles that lodge into the ground along a line. The missiles explode after a delay or upon detecting an enemy, dealing X and marking the target. Shooting a marked enemy with a charged shot. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 15. Ability icon: Chimera Mk1

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_chimera_mk1.png`, replaces `panorama/images/hud/abilities/deadpack/deadpack_chimera_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Chimera Mk1", an ability of {{CHARACTER}} from the reference sheet: Hold X to fly, building up heat. When max heat is reached, your jetpack will begin to overheat. Overheating: After a short delay, dive to the ground. Upon impact, create an explosion that deals damage and launches you. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 16. Ability icon: Jettison Fuel

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_jettison_fuel.png`, replaces `panorama/images/hud/abilities/deadpack/deadpack_thermalmass_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Jettison Fuel", an ability of {{CHARACTER}} from the reference sheet: Spray fuel dealing X and shredding X, building up to a huge backblast that deals X and knocks you backwards. Vents the jetpack when dealing damage. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 17. Ability icon: Close The Deal

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_close_the_deal.png`, replaces `panorama/images/hud/abilities/deadpack/deadpack_flight_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Close The Deal", an ability of {{CHARACTER}} from the reference sheet: Grab an enemy and give them a hands-on demonstration, flying high into the air before crashing back to the ground. You and the participant take X while falling and X upon impact. Damage is increased to enemies under X%. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 18. Ability model: deadpack_globules (optional)

- Tool: `meshy_text_to_3d`, model `meshy-t2.5`, about 15 credits
- Options: `refine: true`, `target_polycount: 10000`
- Save as: `assets/prop_deadpack_globules.glb`, replaces `models/abilities/deadpack_globules.vmdl`
- Thrown and placed ability props (bombs, statues, traps) are replaced this way; dlmod prop pins it to the prop's root bone.

```text
A game-ready 3D prop for {{CONCEPT}}: it replaces Deadman Danny's "deadpack globules" ability model. Single object, stylized PBR in the character's colours, centered, no base, no characters.
```

## 19. Ability model: deadpack_missile_model (optional)

- Tool: `meshy_text_to_3d`, model `meshy-t2.5`, about 15 credits
- Options: `refine: true`, `target_polycount: 10000`
- Save as: `assets/prop_deadpack_missile_model.glb`, replaces `models/abilities/deadpack_missile_model.vmdl`
- Thrown and placed ability props (bombs, statues, traps) are replaced this way; dlmod prop pins it to the prop's root bone.

```text
A game-ready 3D prop for {{CONCEPT}}: it replaces Deadman Danny's "deadpack missile model" ability model. It is used by "Strix Salvo" (Launch a salvo of spirit missiles that lodge into the ground along a line. The missiles explode after a delay or upon…). Single object, stylized PBR in the character's colours, centered, no base, no characters.
```

