# Calico: Meshy prompts

Replace `{{CONCEPT}}` with a description of your character, for example *Rook, a hulking stone golem with glowing blue runes and a lantern for a heart*, and `{{CHARACTER}}` with its short name (*Rook*). Or set `concept` and `select.name` in `workspace.json` and run `dlmod meshy nano` again to fill them in.

Everything: about 206 credits (156 without the optional steps). Prices are Meshy's as of 2026-10; confirm before generating.

## 1. Character design sheet

- Tool: `meshy_text_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 16:9
- Save as: `assets/design_sheet.png`
- Every later picture uses this sheet as its reference, so redo it until the character is right.

```text
Character design sheet for a 3D game character: {{CONCEPT}}. It replaces Calico in Deadlock, a stylized third-person hero shooter (Calico: Wreaks havoc from the sidelines). Its abilities are Gloom Bombs, Leaping Slash, Ava, Return to Shadows. The character must read at a glance from behind in third person, so give it a strong silhouette and one or two bold colours. It carries its own take on Calico's weapon, Sweetest Lie. Show a large hero render, a front / three-quarter / side / back turnaround in a relaxed A-pose, close-ups of the head, hands and weapon, a colour palette with hex codes, and its two or three iconic symbols. Stylized PBR, clean studio light, dark background, labels only.
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
- Save as: `assets/model.glb`, replaces `models/heroes_staging/nano/nano_v2/nano.vmdl`
- Detailed geometry reads best close up in third person, and Meshy models of about 200k triangles install fine. dlmod scales it to Calico's height (95 units) and rigs it to Calico's skeleton. For a lighter model use meshy_image_to_3d with meshy-t2.5 + meshy-n1 (15 credits, target_polycount 60000) on the front view.

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
A single game-ready prop: the signature weapon of {{CONCEPT}}. It is its own take on Calico's weapon, Sweetest Lie. Stylized PBR in the character's colours, centered, no hands, no base.
```

## 6. Hero select screen art

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 16:9
- Reference: `assets/design_sheet.png`
- Save as: `assets/select.png`, replaces `panorama/images/heroes/backgrounds/calico_bg_psd.vtex_c` (4096x2312)
- dlmod select hides the hero's 3D scene so the picture shows on its own (scene=hide) and fades the left side to black under the hero grid.

```text
Splash art for a hero select screen: {{CHARACTER}} from the reference sheet in a setting that shows off its personality and its part in a team (wreaks havoc from the sidelines). Put the character in the RIGHT half of the frame, its centre at about 70% of the width and its head in the top third; keep the LEFT 45% calm and dark, it sits under the hero grid; keep the bottom-right corner simple, the name, tags and ability icons go there. Cinematic light, rich detail, no text, no logos.
```

## 7. Hero card

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 3:4
- Reference: `assets/design_sheet.png`
- Save as: `assets/card.png`, replaces `panorama/images/heroes/nano_card_psd.vtex_c` (280x380)
- dlmod apply keys out the green background.

```text
Hero card portrait of {{CHARACTER}} from the reference sheet: chest-up, three-quarter view, facing the viewer with confidence, its weapon in view, dramatic rim light, stylized 3D render, centered with some headroom. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 8. Card at low health

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 3:4
- Reference: `assets/design_sheet.png`
- Save as: `assets/card_critical.png`, replaces `panorama/images/heroes/nano_card_critical_psd.vtex_c` (280x380)
- dlmod apply keys out the green background.

```text
The same hero card portrait of {{CHARACTER}} from the reference sheet, badly hurt: scuffed and cracked, cringing but defiant, red danger light, same framing. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 9. Card after a kill

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 3:4
- Reference: `assets/design_sheet.png`
- Save as: `assets/card_gloat.png`, replaces `panorama/images/heroes/nano_card_gloat_psd.vtex_c` (280x380)
- dlmod apply keys out the green background.

```text
The same hero card portrait of {{CHARACTER}} from the reference sheet, triumphant: a taunting gesture after a kill, warm golden light, same framing. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 10. Tall portrait

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 9:16
- Reference: `assets/design_sheet.png`
- Save as: `assets/vertical.png`, replaces `panorama/images/heroes/nano_vertical_psd.vtex_c` (120x200)
- dlmod apply keys out the green background.

```text
Full-body portrait of {{CHARACTER}} from the reference sheet standing tall in its signature pose, the whole character in frame. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 11. Small portrait

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/sm.png`, replaces `panorama/images/heroes/nano_sm_psd.vtex_c` (128x128)
- dlmod apply keys out the green background.

```text
Head-and-shoulders portrait of {{CHARACTER}} from the reference sheet, front-facing, big simple shapes that read when small. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 12. Minimap icon

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/mm.png`, replaces `panorama/images/heroes/nano_mm_psd.vtex_c` (50x50)
- dlmod apply keys out the green background.

```text
Minimap icon of {{CHARACTER}} from the reference sheet: the head only, front view, flat colours, thick dark outline, readable at 32 pixels. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 13. Weapon picture

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 16:9
- Reference: `assets/design_sheet.png`
- Save as: `assets/gun.png`, replaces `panorama/images/heroes/guns/nano_gun_psd.vtex_c` (600x300)
- dlmod apply keys out the green background.

```text
Side view of the weapon or held prop of {{CHARACTER}} from the reference sheet, alone, horizontal, stylized 3D render. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 14. Ability icon: Gloom Bombs

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_gloom_bombs.png`, replaces `panorama/images/hud/abilities/nano/nano_clustergrenade_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Gloom Bombs", an ability of {{CHARACTER}} from the reference sheet: Throw out cluster bombs. Throw a cluster of bombs that detonate after a delay, dealing X. Enemies hit by multiple bombs take X% damage. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 15. Ability icon: Leaping Slash

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_leaping_slash.png`, replaces `panorama/images/hud/abilities/nano/nano_dash_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Leaping Slash", an ability of {{CHARACTER}} from the reference sheet: Dash forward and slash enemies. Dash forward before slashing all enemies in a circle, dealing X. If the ability hits at least one hero, X a small amount of health. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 16. Ability icon: Ava

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_ava.png`, replaces `panorama/images/hud/abilities/nano/nano_catform_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Ava", an ability of {{CHARACTER}} from the reference sheet: Take control of Ava. Turn to shadows and possess Ava. You gain X that increases over time, and become hidden on the minimap. Taking damage from an enemy hero resets your bonus move speed and puts Ava on a brief cooldown. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 17. Ability icon: Return to Shadows

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_return_to_shadows.png`, replaces `panorama/images/hud/abilities/nano/nano_shadow_pulse_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Return to Shadows", an ability of {{CHARACTER}} from the reference sheet: Turn to Shadows, gaining movespeed and dealing delayed damage. Instantly turn to shadows, becoming untargetable, gaining X, and dealing X. After a delay, return from the shadows, dealing X again. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 18. Ability model: nano_catform_model (optional)

- Tool: `meshy_text_to_3d`, model `meshy-t2.5`, about 15 credits
- Options: `refine: true`, `target_polycount: 10000`
- Save as: `assets/prop_nano_catform_model.glb`, replaces `models/abilities/nano_catform_model.vmdl`
- Thrown and placed ability props (bombs, statues, traps) are replaced this way; dlmod prop pins it to the prop's root bone.

```text
A game-ready 3D prop for {{CONCEPT}}: it replaces Calico's "nano catform model" ability model. It is used by "Ava" (Take control of Ava). Single object, stylized PBR in the character's colours, centered, no base, no characters.
```

## 19. Ability model: nano_ritual_target_swirl (optional)

- Tool: `meshy_text_to_3d`, model `meshy-t2.5`, about 15 credits
- Options: `refine: true`, `target_polycount: 10000`
- Save as: `assets/prop_nano_ritual_target_swirl.glb`, replaces `models/abilities/nano_ritual_target_swirl.vmdl`
- Thrown and placed ability props (bombs, statues, traps) are replaced this way; dlmod prop pins it to the prop's root bone.

```text
A game-ready 3D prop for {{CONCEPT}}: it replaces Calico's "nano ritual target swirl" ability model. Single object, stylized PBR in the character's colours, centered, no base, no characters.
```

