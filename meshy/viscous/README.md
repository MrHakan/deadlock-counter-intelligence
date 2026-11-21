# Viscous (`viscous`)

Rolls around deflecting bullets. Viscous vexes his opponents through clever evasion and strategic uses of healing, enabling him to withstand the most brutal of enemy attacks.

Weapon: Deep Sea Interpreter. Tags: Evasive, Disruptor, Gooey.

Make a replacement character with [prompts.md](prompts.md), save each result in `assets/`, then install everything that is there:

```powershell
dlmod apply meshy/viscous
```

Set the character's description (`concept`), the name and tags shown on the hero select screen and the tag colours in `workspace.json` first.

## Abilities

| Ability | What it does | Icon | Ability models |
| --- | --- | --- | --- |
| Splatter | Damage enemies and make a mess | `viscous_goo_ball_psd.vtex_c` | `models/abilities/viscous_puddle_punch_model.vmdl`, `models/abilities/viscous_puddle_telegraph_fx.vmdl` |
| The Cube | The Cube is a safe place to be | `viscous_restorative_goo_psd.vtex_c` | `models/abilities/viscous_cube.vmdl` |
| Puddle Punch | Punch enemies with a giant fist | `viscous_goo_punch_psd.vtex_c` | `models/abilities/viscous_fist_melt.vmdl`, `models/abilities/viscous_fist_model.vmdl`, `models/abilities/viscous_puddle_punch_model.vmdl`, `models/abilities/viscous_puddle_telegraph_fx.vmdl` |
| Goo Ball | Roll around and cause chaos | `viscous_goo_sphere_psd.vtex_c` |  |

## Game files

| What | File | Size |
| --- | --- | --- |
| Model | `models/heroes_staging/viscous/viscous.vmdl` | 119 units tall |
| Animation skeleton | `models/heroes_staging/kelvin_v2/maya/kelvin.vnmskel` | |
| Hero card (hero select, HUD) | `panorama/images/heroes/viscous_card_psd.vtex_c` | 280x380 |
| Card at low health | `panorama/images/heroes/viscous_card_critical_psd.vtex_c` | 280x380 |
| Card after a kill | `panorama/images/heroes/viscous_card_gloat_psd.vtex_c` | 280x380 |
| Tall portrait | `panorama/images/heroes/viscous_vertical_psd.vtex_c` | 120x200 |
| Small portrait | `panorama/images/heroes/viscous_sm_psd.vtex_c` | 128x128 |
| Minimap icon | `panorama/images/heroes/viscous_mm_psd.vtex_c` | 50x50 |
| Background | `panorama/images/heroes/backgrounds/viscous_bg_psd.vtex_c` | 4096x2312 |
| Weapon picture | `panorama/images/heroes/guns/viscous_gun_psd.vtex_c` | 600x300 |
| Ability icon | `panorama/images/hud/abilities/viscous/viscous_goo_ball_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/viscous/viscous_goo_puddle_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/viscous/viscous_goo_punch_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/viscous/viscous_goo_sphere_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/viscous/viscous_restorative_goo_psd.vtex_c` | 137x137 |
| Name logo (SVG) | `panorama/images/heroes/hero_names/viscous.vsvg_c` | |
| Name logo (SVG) | `panorama/images/heroes/hero_names/viscous_localized.vsvg_c` | |
| Hero select 3D scene | `game/citadel/maps/ui/hero_prefabs/viscous.vpk` | |
| Ability model | `models/abilities/viscous_cube.vmdl` | |
| Ability model | `models/abilities/viscous_fist_melt.vmdl` | |
| Ability model | `models/abilities/viscous_fist_model.vmdl` | |
| Ability model | `models/abilities/viscous_puddle_punch_model.vmdl` | |
| Ability model | `models/abilities/viscous_puddle_telegraph_fx.vmdl` | |
| Ability model | `models/abilities/viscous_sphere.vmdl` | |
| Effects | `particles/abilities/viscous/` | |
| Effects | `particles/weapon_fx/viscous/` | |
| Name and tag text | `Steam_RP_hero_viscous`, `hero_viscous:n`, `hero_viscous_search:n`, `Citadel_Viscous_HeroTag_1`, `Citadel_Viscous_HeroTag_2`, `Citadel_Viscous_HeroTag_3` | `citadel_heroes`, `citadel_gc_hero_names` |

## Animations

The game plays Viscous's own 0 clips on whatever model is installed, so a replacement is rigged to Viscous's bones rather than given new animations. What the game does to those clips matters:

- Melee, reloads and casts are laid over the movement through bone masks (`FullBody`, `Throw_UpperBody`, `Reload_Zoomed`, `Reload_Idle`, `Reload_InAir`, `Reload_Slide`, `Reload_Haspath`, `Reload_HasPathCrouch`, `Reload_Crouch`, `Turn_LowerBody`, `zero`, `root`, `UpperBodyReplace_InAir`, `UpperBodyReplace_Turn`, `UpperBodyReplace_Slide`, `UpperBodyReplace_AirDash`, `UpperBodyReplace_GroundDash`, `melee_UpperBody`, `melee_dash`, `Reload_GunOnly`, `Cast_UpperBody`, `UpperBodyReplace_Asleep_Channeling`, `UpperBodyReplace_Asleep`, `UpperBodyReplace_Shopping`, `Reload_HasPathCrouch_ALT`, `Reload_HasPath_ALT`, `Reload_Crouch_ALT`, `Parry_UpperBody`, `Reload_InAir_ALT`, `Reload_Slide_ALT`, `Left_Arm_ALL`): only the upper body takes the clip, so a fit to the whole clip is wrong in game.
- The hero graph solves both arms by IK towards the gun (`weaponHand_L`, `weaponHand_R`) and the legs towards `leg_*_IKTARGET`, so the arm bones in game are not what the clips say. Body parts that should follow the hero safely use `head`, `neck_0`, the spine, the clavicles, `pelvis` and the leg IK targets.
- `attachWorld` never moves: weight on it keeps a part steady. `root_motion` is the character's place; its rest pose in the file is turned, which dlmod corrects.
- The third-person camera looks from behind and close: a wide character hides what happens in front of it, and a body moved backwards puts the camera inside it.

## Install by hand

```powershell
dlmod model viscous assets/model.glb
dlmod select viscous bg=assets/select.png name=NAME tags=A,B,C colors=#7A3CFF,#FFB020 scene=hide
dlmod icons viscous 1=assets/icon_....png
dlmod image panorama/images/heroes/viscous_card_psd.vtex_c assets/card.png
dlmod prop models/abilities/viscous_cube.vmdl assets/prop_viscous_cube.glb
dlmod paint viscous
```
