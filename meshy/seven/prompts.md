# Seven: Meshy prompts

Replace `{{CONCEPT}}` with a description of your character, for example *Rook, a hulking stone golem with glowing blue runes and a lantern for a heart*, and `{{CHARACTER}}` with its short name (*Rook*). Or set `concept` and `select.name` in `workspace.json` and run `dlmod meshy gigawatt` again to fill them in.

Everything: about 176 credits (156 without the optional steps). Prices are Meshy's as of 2026-10; confirm before generating.

## 1. Character design sheet

- Tool: `meshy_text_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 16:9
- Save as: `assets/design_sheet.png`
- Every later picture uses this sheet as its reference, so redo it until the character is right.

```text
Character design sheet for a 3D game character: {{CONCEPT}}. It replaces Seven in Deadlock, a stylized third-person hero shooter (Seven: Electrocutes crowds of enemies). Its abilities are Lightning Ball, Static Charge, Power Surge, Storm Cloud. The character must read at a glance from behind in third person, so give it a strong silhouette and one or two bold colours. It carries its own take on Seven's weapon, Cold Calculus. Show a large hero render, a front / three-quarter / side / back turnaround in a relaxed A-pose, close-ups of the head, hands and weapon, a colour palette with hex codes, and its two or three iconic symbols. Stylized PBR, clean studio light, dark background, labels only.
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
- Save as: `assets/model.glb`, replaces `models/heroes_staging/gigawatt_prisoner/gigawatt_prisoner.vmdl`
- Detailed geometry reads best close up in third person, and Meshy models of about 200k triangles install fine. dlmod scales it to Seven's height (113 units) and rigs it to Seven's skeleton. For a lighter model use meshy_image_to_3d with meshy-t2.5 + meshy-n1 (15 credits, target_polycount 60000) on the front view.

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
A single game-ready prop: the signature weapon of {{CONCEPT}}. It is its own take on Seven's weapon, Cold Calculus. Stylized PBR in the character's colours, centered, no hands, no base.
```

## 6. Hero select screen art

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 16:9
- Reference: `assets/design_sheet.png`
- Save as: `assets/select.png`, replaces `panorama/images/heroes/backgrounds/seven_bg_psd.vtex_c` (4096x2312)
- dlmod select hides the hero's 3D scene so the picture shows on its own (scene=hide) and fades the left side to black under the hero grid.

```text
Splash art for a hero select screen: {{CHARACTER}} from the reference sheet in a setting that shows off its personality and its part in a team (electrocutes crowds of enemies). Put the character in the RIGHT half of the frame, its centre at about 70% of the width and its head in the top third; keep the LEFT 45% calm and dark, it sits under the hero grid; keep the bottom-right corner simple, the name, tags and ability icons go there. Cinematic light, rich detail, no text, no logos.
```

## 7. Hero card

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 3:4
- Reference: `assets/design_sheet.png`
- Save as: `assets/card.png`, replaces `panorama/images/heroes/gigawatt_card_psd.vtex_c` (280x380)
- dlmod apply keys out the green background.

```text
Hero card portrait of {{CHARACTER}} from the reference sheet: chest-up, three-quarter view, facing the viewer with confidence, its weapon in view, dramatic rim light, stylized 3D render, centered with some headroom. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 8. Card at low health

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 3:4
- Reference: `assets/design_sheet.png`
- Save as: `assets/card_critical.png`, replaces `panorama/images/heroes/gigawatt_card_critical_psd.vtex_c` (280x380)
- dlmod apply keys out the green background.

```text
The same hero card portrait of {{CHARACTER}} from the reference sheet, badly hurt: scuffed and cracked, cringing but defiant, red danger light, same framing. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 9. Card after a kill

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 3:4
- Reference: `assets/design_sheet.png`
- Save as: `assets/card_gloat.png`, replaces `panorama/images/heroes/gigawatt_card_gloat_psd.vtex_c` (280x380)
- dlmod apply keys out the green background.

```text
The same hero card portrait of {{CHARACTER}} from the reference sheet, triumphant: a taunting gesture after a kill, warm golden light, same framing. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 10. Tall portrait

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 9:16
- Reference: `assets/design_sheet.png`
- Save as: `assets/vertical.png`, replaces `panorama/images/heroes/gigawatt_vertical_psd.vtex_c` (120x200)
- dlmod apply keys out the green background.

```text
Full-body portrait of {{CHARACTER}} from the reference sheet standing tall in its signature pose, the whole character in frame. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 11. Small portrait

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/sm.png`, replaces `panorama/images/heroes/gigawatt_sm_psd.vtex_c` (128x128)
- dlmod apply keys out the green background.

```text
Head-and-shoulders portrait of {{CHARACTER}} from the reference sheet, front-facing, big simple shapes that read when small. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 12. Minimap icon

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/mm.png`, replaces `panorama/images/heroes/gigawatt_mm_psd.vtex_c` (50x50)
- dlmod apply keys out the green background.

```text
Minimap icon of {{CHARACTER}} from the reference sheet: the head only, front view, flat colours, thick dark outline, readable at 32 pixels. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 13. Weapon picture

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 16:9
- Reference: `assets/design_sheet.png`
- Save as: `assets/gun.png`, replaces `panorama/images/heroes/guns/gigawatt_gun_psd.vtex_c` (600x300)
- dlmod apply keys out the green background.

```text
Side view of the weapon or held prop of {{CHARACTER}} from the reference sheet, alone, horizontal, stylized 3D render. On a flat pure green (#00FF00) background, no green on the character, no text.
```

## 14. Ability icon: Lightning Ball

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_lightning_ball.png`, replaces `panorama/images/hud/abilities/giga_ball_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Lightning Ball", an ability of {{CHARACTER}} from the reference sheet: Launch a damaging ball of lightning. Shoot a ball of lightning that travels in a straight line. Does damage to all targets in its radius. Slows down when damaging enemies and stops if it hits the world. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 15. Ability icon: Static Charge

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_static_charge.png`, replaces `panorama/images/hud/abilities/giga_static_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Static Charge", an ability of {{CHARACTER}} from the reference sheet: Apply a delayed area stun to an enemy hero. Apply a charge to a target enemy hero. After a short duration, the static charge stuns and damages enemies within the radius. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 16. Ability icon: Power Surge

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_power_surge.png`, replaces `panorama/images/hud/abilities/giga_chain_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Power Surge", an ability of {{CHARACTER}} from the reference sheet: Create electric arcs with your shots. Power up your weapon with a shock effect, making your bullets proc shock damage on your target. This shock damage bounces to enemies near your target. Occurs once per burst shot. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

## 17. Ability icon: Storm Cloud

- Tool: `meshy_image_to_image`, model `nano-banana-pro`, about 9 credits
- Aspect ratio: 1:1
- Reference: `assets/design_sheet.png`
- Save as: `assets/icon_storm_cloud.png`, replaces `panorama/images/hud/abilities/giga_storm_psd.vtex_c` (137x137)
- dlmod turns it into a white glyph on transparency.

```text
A single game ability icon glyph for "Storm Cloud", an ability of {{CHARACTER}} from the reference sheet: Fry all enemies in a large area. Channel an expanding storm cloud around you that damages all enemies within its radius. Enemies do not take damage when they are out of line-of-sight. Draw the idea with the character's own shapes and props, not its whole body. Flat solid pure white silhouette with a few clean cut-out interior lines, bold readable shapes, centered, filling about 80% of the frame, on a pure flat black background. No gradients, no shading, no colour, no text, no border, no circle frame.
```

