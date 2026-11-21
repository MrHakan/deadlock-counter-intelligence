# Lash (`lash`)

Dives down to stomp his foes. Lash gives his enemies little room to breathe, either by flying in from long distances to initiate combat with unsuspecting enemies, or pursuing said enemies when they try to run.

Weapon: Tale of The Tape. Tags: Initiator, High-Flying, Arrogant.

Make a replacement character with [prompts.md](prompts.md), save each result in `assets/`, then install everything that is there:

```powershell
dlmod apply meshy/lash
```

Set the character's description (`concept`), the name and tags shown on the hero select screen and the tag colours in `workspace.json` first.

## Abilities

| Ability | What it does | Icon | Ability models |
| --- | --- | --- | --- |
| Ground Strike | Slam down to the ground, damaging enemies | `lash_death_slam_psd.vtex_c` | `models/abilities/lash_ground_strike_fx.vmdl` |
| Grapple | Use your whip to pull yourself to an enemy | `lash_lash_psd.vtex_c` | `models/abilities/lash_flog_whip_fx.vmdl` |
| Flog | Heals from targets ahead of you | `lash_flog_psd.vtex_c` | `models/abilities/lash_flog_whip_fx.vmdl`, `models/abilities/lash_ground_strike_fx.vmdl` |
| Death Slam | Lift enemies and smash them into the ground | `lash_counter_lash_psd.vtex_c` | `models/abilities/lash_ground_strike_fx.vmdl` |

## Game files

| What | File | Size |
| --- | --- | --- |
| Model | `models/heroes_wip/lash/lash.vmdl` | 105 units tall |
| Animation skeleton | `models/heroes_wip/lash/lash.vnmskel` | |
| Hero card (hero select, HUD) | `panorama/images/heroes/lash_card_psd.vtex_c` | 280x380 |
| Card at low health | `panorama/images/heroes/lash_card_critical_psd.vtex_c` | 280x380 |
| Card after a kill | `panorama/images/heroes/lash_card_gloat_psd.vtex_c` | 280x380 |
| Tall portrait | `panorama/images/heroes/lash_vertical_psd.vtex_c` | 120x200 |
| Small portrait | `panorama/images/heroes/lash_sm_psd.vtex_c` | 128x128 |
| Minimap icon | `panorama/images/heroes/lash_mm_psd.vtex_c` | 50x50 |
| Background | `panorama/images/heroes/backgrounds/lash_bg_psd.vtex_c` | 4096x2312 |
| Weapon picture | `panorama/images/heroes/guns/lash_gun_psd.vtex_c` | 600x300 |
| Ability icon | `panorama/images/hud/abilities/lash/lash_counter_lash_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/lash/lash_death_slam_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/lash/lash_flog_psd.vtex_c` | 137x137 |
| Ability icon | `panorama/images/hud/abilities/lash/lash_lash_psd.vtex_c` | 137x137 |
| Name logo (SVG) | `panorama/images/heroes/hero_names/lash.vsvg_c` | |
| Name logo (SVG) | `panorama/images/heroes/hero_names/lash_localized.vsvg_c` | |
| Hero select 3D scene | `game/citadel/maps/ui/hero_prefabs/lash.vpk` | |
| Ability model | `models/abilities/lash_flog_whip_fx.vmdl` | |
| Ability model | `models/abilities/lash_ground_strike_fx.vmdl` | |
| Effects | `particles/abilities/lash/` | |
| Effects | `particles/weapon_fx/lash/` | |
| Name and tag text | `Steam_RP_hero_lash`, `hero_lash:n`, `hero_lash_search:n`, `Citadel_Lash_HeroTag_1`, `Citadel_Lash_HeroTag_2`, `Citadel_Lash_HeroTag_3` | `citadel_heroes`, `citadel_gc_hero_names` |

## Animations

The game plays Lash's own 0 clips on whatever model is installed, so a replacement is rigged to Lash's bones rather than given new animations. What the game does to those clips matters:

- Melee, reloads and casts are laid over the movement through bone masks (`Cast_UpperBody`, `FullBody`, `melee_dash`, `melee_UpperBody`, `Parry_UpperBody`, `Reload_Crouch`, `Reload_GunOnly`, `Reload_Haspath`, `Reload_HasPathCrouch`, `Reload_Idle`, `Reload_InAir`, `Reload_Slide`, `Reload_Zoomed`, `root`, `Throw_UpperBody`, `Turn_LowerBody`, `UpperBodyReplace_AirDash`, `UpperBodyReplace_Asleep`, `UpperBodyReplace_Asleep_Channeling`, `UpperBodyReplace_GroundDash`, `UpperBodyReplace_InAir`, `UpperBodyReplace_Shopping`, `UpperBodyReplace_Slide`, `UpperBodyReplace_Turn`, `zero`): only the upper body takes the clip, so a fit to the whole clip is wrong in game.
- The hero graph solves both arms by IK towards the gun (`weaponHand_L`, `weaponHand_R`) and the legs towards `leg_*_IKTARGET`, so the arm bones in game are not what the clips say. Body parts that should follow the hero safely use `head`, `neck_0`, the spine, the clavicles, `pelvis` and the leg IK targets.
- `attachWorld` never moves: weight on it keeps a part steady. `root_motion` is the character's place; its rest pose in the file is turned, which dlmod corrects.
- The third-person camera looks from behind and close: a wide character hides what happens in front of it, and a body moved backwards puts the camera inside it.

## Install by hand

```powershell
dlmod model lash assets/model.glb
dlmod select lash bg=assets/select.png name=NAME tags=A,B,C colors=#7A3CFF,#FFB020 scene=hide
dlmod icons lash 1=assets/icon_....png
dlmod image panorama/images/heroes/lash_card_psd.vtex_c assets/card.png
dlmod prop models/abilities/lash_flog_whip_fx.vmdl assets/prop_lash_flog_whip_fx.glb
dlmod paint lash
```
