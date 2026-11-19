package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// A profile is a named set of switched-on mods, such as "Ranked" or "Just for fun". Using one
// switches every other mod off; switched-off mods stay installed.

// adopt gives mods added by hand a dlmod ID so profiles can refer to them.
func (s *Store) adopt(entries []*Entry) {
	for _, entry := range entries {
		if entry.Mod == nil {
			entry.Mod = &Mod{ID: s.manifest.NextID, Name: entry.Name(), Slot: entry.Slot, Enabled: true}
			s.manifest.NextID++
			s.manifest.Mods = append(s.manifest.Mods, entry.Mod)
		}
	}
}

func (s *Store) profileNames() []string {
	names := make([]string, 0, len(s.manifest.Profiles))
	for name := range s.manifest.Profiles {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return names
}

// findProfile matches a name without regard to case.
func (s *Store) findProfile(name string) (string, bool) {
	for existing := range s.manifest.Profiles {
		if strings.EqualFold(existing, strings.TrimSpace(name)) {
			return existing, true
		}
	}
	return "", false
}

// saveProfile records the mods that are on now, replacing a profile with the same name.
func (s *Store) saveProfile(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("give the profile a name")
	}
	entries, err := s.entries()
	if err != nil {
		return 0, err
	}
	s.adopt(entries)
	on := []int{}
	for _, entry := range entries {
		if entry.Enabled() {
			on = append(on, entry.Mod.ID)
		}
	}
	if existing, ok := s.findProfile(name); ok {
		delete(s.manifest.Profiles, existing)
	}
	if s.manifest.Profiles == nil {
		s.manifest.Profiles = map[string][]int{}
	}
	s.manifest.Profiles[name] = on
	return len(on), s.save()
}

// useProfile switches on exactly the mods in the profile. Mods are switched off first, which
// frees their slots for the ones being switched on.
func (s *Store) useProfile(p prompter, name string) error {
	key, ok := s.findProfile(name)
	if !ok {
		return fmt.Errorf("there is no profile named %q", name)
	}
	want := map[int]bool{}
	for _, id := range s.manifest.Profiles[key] {
		want[id] = true
	}
	entries, err := s.entries()
	if err != nil {
		return err
	}
	s.adopt(entries)
	off, on := 0, 0
	for _, entry := range entries {
		if entry.Enabled() && !want[entry.Mod.ID] {
			if err := s.disable(entry); err != nil {
				return err
			}
			off++
		}
	}
	found := map[int]bool{}
	for _, entry := range entries {
		found[entry.Mod.ID] = true
		if !entry.Enabled() && want[entry.Mod.ID] {
			if err := s.enable(entry); err != nil {
				return err
			}
			on++
		}
	}
	missing := 0
	for id := range want {
		if !found[id] {
			missing++
		}
	}
	if on == 0 && off == 0 {
		p.say("Profile '%s' is already in use.", key)
	} else {
		p.say("Using profile '%s': %d switched on, %d switched off.", key, on, off)
	}
	if missing > 0 {
		p.say("Warning: %s in this profile %s no longer installed.", plural(missing, "%d mod", "%d mods"),
			map[bool]string{true: "is", false: "are"}[missing == 1])
	}
	return s.save()
}

func (s *Store) deleteProfile(name string) error {
	key, ok := s.findProfile(name)
	if !ok {
		return fmt.Errorf("there is no profile named %q", name)
	}
	delete(s.manifest.Profiles, key)
	return s.save()
}

// activeProfile names the profile whose mods are exactly the ones switched on, if any.
func (s *Store) activeProfile(entries []*Entry) string {
	on := map[int]bool{}
	for _, entry := range entries {
		if entry.Enabled() {
			if entry.Mod == nil {
				return "" // a mod added by hand is not part of any profile yet
			}
			on[entry.Mod.ID] = true
		}
	}
	for _, name := range s.profileNames() {
		ids := s.manifest.Profiles[name]
		if len(ids) != len(on) {
			continue
		}
		match := true
		for _, id := range ids {
			match = match && on[id]
		}
		if match {
			return name
		}
	}
	return ""
}
