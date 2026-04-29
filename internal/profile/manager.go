package profile

import (
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

type Manager struct {
	mu       sync.RWMutex
	profiles []Profile
	byName   map[string]*Profile
}

func NewManager(userProfiles []Profile, defaultProfiles []Profile) *Manager {
	m := &Manager{
		profiles: make([]Profile, 0),
		byName:   make(map[string]*Profile),
	}
	m.MergeDefaults(defaultProfiles)
	m.MergeUser(userProfiles)
	return m
}

func (m *Manager) MergeDefaults(defaults []Profile) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range defaults {
		name := defaults[i].Name
		if _, exists := m.byName[name]; !exists {
			p := defaults[i]
			if p.Enabled {
				m.profiles = append(m.profiles, p)
				m.byName[name] = &m.profiles[len(m.profiles)-1]
			}
		}
	}
	m.sortByPriority()
}

func (m *Manager) MergeUser(user []Profile) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mergeUserUnlocked(user)
	m.sortByPriority()
}

func (m *Manager) mergeUserUnlocked(user []Profile) {
	for i := range user {
		name := user[i].Name
		if existing, ok := m.byName[name]; ok {
			*existing = user[i]
			existing.Enabled = user[i].Enabled
		} else {
			p := user[i]
			if p.Enabled {
				m.profiles = append(m.profiles, p)
				m.byName[name] = &m.profiles[len(m.profiles)-1]
			}
		}
	}
}

func (m *Manager) ReplaceUser(profiles []Profile) {
	m.mu.Lock()
	defer m.mu.Unlock()

	filtered := make([]Profile, 0)
	for _, p := range m.profiles {
		if !p.isDefault {
			continue
		}
		filtered = append(filtered, p)
	}

	m.profiles = filtered
	m.byName = make(map[string]*Profile, len(m.profiles))
	for i := range m.profiles {
		m.byName[m.profiles[i].Name] = &m.profiles[i]
	}

	m.mergeUserUnlocked(profiles)
	m.sortByPriority()
}

func (m *Manager) All() []Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Profile, len(m.profiles))
	copy(result, m.profiles)
	return result
}

func (m *Manager) Get(name string) *Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byName[name]
}

func (m *Manager) Add(p Profile) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.byName[p.Name]; ok {
		*existing = p
		existing.Enabled = p.Enabled
	} else {
		p.Enabled = true
		m.profiles = append(m.profiles, p)
		m.byName[p.Name] = &m.profiles[len(m.profiles)-1]
	}
	m.sortByPriority()
}

func (m *Manager) Delete(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := -1
	for i := range m.profiles {
		if m.profiles[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	m.profiles = append(m.profiles[:idx], m.profiles[idx+1:]...)
	delete(m.byName, name)
	return true
}

func (m *Manager) sortByPriority() {
	sort.SliceStable(m.profiles, func(i, j int) bool {
		if m.profiles[i].Priority != m.profiles[j].Priority {
			return m.profiles[i].Priority > m.profiles[j].Priority
		}
		return len(m.profiles[i].Match.Value) > len(m.profiles[j].Match.Value)
	})
}

func (m *Manager) Match(processes []DetectedProcess) (*Profile, *DetectedProcess) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return FindBestMatch(m.profiles, processes)
}

func SerializeProfiles(profiles []Profile) ([]byte, error) {
	return yaml.Marshal(profiles)
}

func DeserializeProfiles(data []byte) ([]Profile, error) {
	var profiles []Profile
	if err := yaml.Unmarshal(data, &profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}
