# Kelvin (`kelvin`)

Freezes enemies in their tracks. Timely heals and splitting a teamfight properly is the difference between an easy victory or a ruinous defeat. Gliding around slowing enemies gives Kelvin's team the maneuvering advantage to make this happen.

Weapon: Expedition's End. Tags: Protector, Explorer, Ice Cold.

Make a replacement character with [prompts.md](prompts.md), save each result in `assets/`, then install everything that is there:

```powershell
dlmod apply meshy/kelvin
```

Set the character's description (`concept`), the name and tags shown on the hero select screen and the tag colours in `workspace.json` first.

## Abilities

| Ability | What it does | Icon | Ability models |
| --- | --- | --- | --- |
| Frost Grenade | Damage and slow enemies | `freezing_grenade_psd.vtex_c` |  |
| Ice Path | Speed around on a floating path of ice | `ice_path_psd.vtex_c` |  |
| Arctic Beam | Apply an escalating slow | `ice_beam_psd.vtex_c` | `models/abilities/kelvin_ice_beam_shield.vmdl` |
| Frozen Shelter | Create an impenetrable healing dome | `frozen_shelter_psd.vtex_c` |  |

## Game files

| What | File | Size |
| --- | --- | --- |
| Model | `models/heroes_staging/kelvin_v2/kelvin.vmdl` | 99 units tall |
| Animation skeleton | `models/heroes_staging/kelvin_v2/maya/kelvin.vnmskel` | |
| Hero card (hero select, HUD) | `panorama/images/heroes/kelvin_card_psd.vtex_c` | 280x380 |
| Card at low health | `panorama/images/heroes/kelvin_card_critical_psd.vtex_c` | 280x380 |
| Card after a kill | `panorama/images/heroes/kelvin_card_gloat_psd.vtex_c` | 280x380 |
| Tall portrait | `panorama/images/heroes/kelvin_vertical_psd.vtex_c` | 120x200 |
| Small portrait | `panorama/images/heroes/kelvin_sm_psd.vtex_c` | 128x128 |
| Minimap icon | `panorama/images/heroes/kelvin_mm_psd.vtex_c` | 50x50 |
| Background | `panorama/images/heroes/backgrounds/kelvin_bg_psd.vtex_c` | 4096x2312 |
| Weapon picture | `panorama/images/heroes/guns/kelvin_gun_psd.vtex_c` | 600x300 |
| Ability icon | `panorama/images/hud/abilities/kelvin/freezing_grenade_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/kelvin/frozen_shelter_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/kelvin/ice_beam_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/kelvin/ice_path_psd.vtex_c` | 137x137 |
| Name logo (SVG) | `panorama/images/heroes/hero_names/kelvin.vsvg_c` | |
| Name logo (SVG) | `panorama/images/heroes/hero_names/kelvin_localized.vsvg_c` | |
| Hero select 3D scene | `game/citadel/maps/ui/hero_prefabs/kelvin.vpk` | |
| Ability model | `models/abilities/kelvin_fx_bomb_model.vmdl` | |
| Ability model | `models/abilities/kelvin_ice_beam_shield.vmdl` | |
| Ability model | `models/abilities/kelvin_ice_projectile_model.vmdl` | |
| Effects | `particles/abilities/kelvin/` | |
| Effects | `particles/weapon_fx/kelvin/` | |
| Name and tag text | `Steam_RP_hero_kelvin`, `hero_kelvin:n`, `hero_kelvin_search:n`, `Citadel_Kelvin_HeroTag_1`, `Citadel_Kelvin_HeroTag_2`, `Citadel_Kelvin_HeroTag_3` | `citadel_heroes`, `citadel_gc_hero_names` |

## Animations

The game plays Kelvin's own 0 clips on whatever model is installed, so a replacement is rigged to Kelvin's bones rather than given new animations. What the game does to those clips matters:

- Melee, reloads and casts are laid over the movement through bone masks (`FullBody`, `Throw_UpperBody`, `Reload_Zoomed`, `Reload_Idle`, `Reload_InAir`, `Reload_Slide`, `Reload_Haspath`, `Reload_HasPathCrouch`, `Reload_Crouch`, `Turn_LowerBody`, `zero`, `root`, `UpperBodyReplace_InAir`, `UpperBodyReplace_Turn`, `UpperBodyReplace_Slide`, `UpperBodyReplace_AirDash`, `UpperBodyReplace_GroundDash`, `melee_UpperBody`, `melee_dash`, `Reload_GunOnly`, `Cast_UpperBody`, `UpperBodyReplace_Asleep_Channeling`, `UpperBodyReplace_Asleep`, `UpperBodyReplace_Shopping`, `Reload_HasPathCrouch_ALT`, `Reload_HasPath_ALT`, `Reload_Crouch_ALT`, `Parry_UpperBody`, `Reload_InAir_ALT`, `Reload_Slide_ALT`, `Left_Arm_ALL`): only the upper body takes the clip, so a fit to the whole clip is wrong in game.
- The hero graph solves both arms by IK towards the gun (`weaponHand_L`, `weaponHand_R`) and the legs towards `leg_*_IKTARGET`, so the arm bones in game are not what the clips say. Body parts that should follow the hero safely use `head`, `neck_0`, the spine, the clavicles, `pelvis` and the leg IK targets.
- `attachWorld` never moves: weight on it keeps a part steady. `root_motion` is the character's place; its rest pose in the file is turned, which dlmod corrects.
- The third-person camera looks from behind and close: a wide character hides what happens in front of it, and a body moved backwards puts the camera inside it.

## Install by hand

```powershell
dlmod model kelvin assets/model.glb
dlmod select kelvin bg=assets/select.png name=NAME tags=A,B,C colors=#7A3CFF,#FFB020 scene=hide
dlmod icons kelvin 1=assets/icon_....png
dlmod image panorama/images/heroes/kelvin_card_psd.vtex_c assets/card.png
dlmod prop models/abilities/kelvin_fx_bomb_model.vmdl assets/prop_kelvin_fx_bomb_model.glb
dlmod paint kelvin
```
