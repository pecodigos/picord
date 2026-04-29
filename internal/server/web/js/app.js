function clearChildren(el) {
  while (el.firstChild) el.removeChild(el.firstChild);
}

function createText(text) {
  return document.createTextNode(text);
}

async function refreshStatus() {
  const s = await get('/api/status');
  document.getElementById('active-name').textContent = s.active_name || 'None';
  document.getElementById('active-proc').textContent = s.active_process || '-';
  document.getElementById('auto-detect-status').textContent = s.auto_detect ? 'On' : 'Off';
  document.getElementById('override-status').textContent = s.has_override ? 'Yes' : 'No';

  const btn = document.getElementById('btn-auto-detect');
  btn.textContent = s.auto_detect ? 'Disable Auto-Detect' : 'Enable Auto-Detect';

  const tbody = document.getElementById('process-tbody');
  clearChildren(tbody);
  if (!s.detected_processes || s.detected_processes.length === 0) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 3;
    td.textContent = 'No processes detected';
    tr.appendChild(td);
    tbody.appendChild(tr);
  } else {
    s.detected_processes.forEach(p => {
      const tr = document.createElement('tr');
      const tdPid = document.createElement('td');
      tdPid.textContent = String(p.pid ?? p.PID);
      const tdName = document.createElement('td');
      tdName.textContent = p.name ?? p.Name;
      const tdTitle = document.createElement('td');
      tdTitle.textContent = p.window_title || '-';
      tr.appendChild(tdPid);
      tr.appendChild(tdName);
      tr.appendChild(tdTitle);
      tbody.appendChild(tr);
    });
  }

  refreshCatalogStatus();
}

async function refreshCatalogStatus() {
  try {
    const cat = await get('/api/catalog/status');
    document.getElementById('catalog-status').textContent = cat.enabled ? 'Enabled' : 'Disabled';
    document.getElementById('catalog-entries').textContent = cat.enabled ? (cat.entry_count ?? '-') : '-';
  } catch {
    document.getElementById('catalog-status').textContent = 'Disabled';
    document.getElementById('catalog-entries').textContent = '-';
  }
}

async function searchCatalog() {
  const query = document.getElementById('catalog-query').value.trim();
  if (!query) return;
  const results = await get('/api/catalog/search?q=' + encodeURIComponent(query));
  renderCatalogResults(results, 'catalog-results');
}

function renderCatalogResults(results, containerId) {
  const container = document.getElementById(containerId);
  clearChildren(container);
  if (!results || results.length === 0) {
    const p = document.createElement('p');
    p.style.color = '#888';
    p.textContent = 'No results found.';
    container.appendChild(p);
    return;
  }
  results.forEach(e => {
    const card = document.createElement('div');
    card.className = 'catalog-result-card';
    card.dataset.entryId = e.id;

    const info = document.createElement('div');
    info.className = 'info';

    const title = document.createElement('span');
    title.className = 'title';
    title.textContent = e.title;

    const meta = document.createElement('span');
    meta.className = 'meta';
    meta.textContent = e.source + (e.release_year ? ' \u00b7 ' + e.release_year : '');

    info.appendChild(title);
    info.appendChild(meta);

    const btnDiv = document.createElement('div');
    const btn = document.createElement('button');
    btn.className = 'small';
    btn.textContent = 'Save Profile';
    btnDiv.appendChild(btn);

    card.appendChild(info);
    card.appendChild(btnDiv);
    container.appendChild(card);
  });
}

// Event delegation for catalog result save buttons.
document.getElementById('catalog-results').addEventListener('click', (e) => {
  if (e.target.matches('button.small') && e.target.textContent === 'Save Profile') {
    const card = e.target.closest('.catalog-result-card');
    if (card && card.dataset.entryId) {
      saveProfileFromCatalog(card.dataset.entryId);
    }
  }
});

async function refreshCatalogSource(source) {
  const btn = event.target;
  const original = btn.textContent;
  btn.textContent = 'Refreshing...';
  btn.disabled = true;
  try {
    await post('/api/catalog/refresh', { source });
    alert('Refreshed ' + source);
    refreshCatalogStatus();
  } catch (err) {
    alert('Refresh failed: ' + err);
  } finally {
    btn.textContent = original;
    btn.disabled = false;
  }
}

async function saveProfileFromCatalog(entryId) {
  try {
    await post('/api/catalog/profiles/from-entry/' + encodeURIComponent(entryId), null);
    alert('Profile saved from catalog entry');
    refreshProfiles();
  } catch (err) {
    alert('Failed to save profile: ' + err);
  }
}

async function saveProfileFromProcess(entryId) {
  if (!entryId) return;
  await saveProfileFromCatalog(entryId);
}

async function fillFromCatalogSearch() {
  const query = document.getElementById('mf-catalog-search').value.trim();
  if (!query) return;
  const results = await get('/api/catalog/search?q=' + encodeURIComponent(query));
  const container = document.getElementById('mf-catalog-results');
  clearChildren(container);
  if (!results || results.length === 0) {
    const p = document.createElement('p');
    p.style.color = '#888';
    p.style.fontSize = '0.85em';
    p.textContent = 'No results.';
    container.appendChild(p);
    return;
  }
  results.forEach(e => {
    const card = document.createElement('div');
    card.className = 'catalog-result-card';
    card.style.padding = '8px';
    card.style.marginBottom = '4px';
    card.dataset.entryId = e.id;
    card.dataset.title = e.title;
    card.dataset.source = e.source;

    const info = document.createElement('div');
    info.className = 'info';

    const title = document.createElement('span');
    title.className = 'title';
    title.textContent = e.title;

    const meta = document.createElement('span');
    meta.className = 'meta';
    meta.textContent = e.source + (e.release_year ? ' \u00b7 ' + e.release_year : '');

    info.appendChild(title);
    info.appendChild(meta);

    const btn = document.createElement('button');
    btn.className = 'small';
    btn.textContent = 'Use';

    card.appendChild(info);
    card.appendChild(btn);
    container.appendChild(card);
  });
}

// Event delegation for modal catalog search Use buttons.
document.getElementById('mf-catalog-results').addEventListener('click', (e) => {
  if (e.target.matches('button.small') && e.target.textContent === 'Use') {
    const card = e.target.closest('.catalog-result-card');
    if (card) {
      fillProfileFromEntry(card.dataset.title, card.dataset.source, card.dataset.entryId);
    }
  }
});

function fillProfileFromEntry(title, source, entryId) {
  document.getElementById('mf-name').value = title;
  document.getElementById('mf-details').value = 'Playing ' + title;
  document.getElementById('mf-large-text').value = title;
  document.getElementById('mf-match-value').value = title.toLowerCase().replace(/[^a-z0-9]+/g, ' ').trim();
}

async function refreshProfiles() {
  const profiles = await get('/api/profiles');
  const tbody = document.getElementById('profiles-tbody');
  clearChildren(tbody);
  if (!profiles || profiles.length === 0) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 5;
    td.textContent = 'No custom profiles yet';
    tr.appendChild(td);
    tbody.appendChild(tr);
    return;
  }
  profiles.forEach(p => {
    const tr = document.createElement('tr');

    const tdName = document.createElement('td');
    tdName.textContent = p.name;

    const tdMatch = document.createElement('td');
    tdMatch.textContent = p.match.type + ': ' + p.match.value;

    const tdPriority = document.createElement('td');
    tdPriority.textContent = String(p.priority);

    const tdEnabled = document.createElement('td');
    const badge = document.createElement('span');
    badge.className = 'badge ' + (p.enabled ? 'badge-on' : 'badge-off');
    badge.textContent = p.enabled ? 'On' : 'Off';
    tdEnabled.appendChild(badge);

    const tdActions = document.createElement('td');
    tdActions.className = 'actions-cell';

    const editBtn = document.createElement('button');
    editBtn.className = 'edit-btn';
    editBtn.textContent = 'Edit';
    editBtn.dataset.name = p.name;

    const delBtn = document.createElement('button');
    delBtn.className = 'delete-btn';
    delBtn.textContent = 'Del';
    delBtn.dataset.name = p.name;

    tdActions.appendChild(editBtn);
    tdActions.appendChild(delBtn);

    tr.appendChild(tdName);
    tr.appendChild(tdMatch);
    tr.appendChild(tdPriority);
    tr.appendChild(tdEnabled);
    tr.appendChild(tdActions);
    tbody.appendChild(tr);
  });
}

// Event delegation for profile edit/delete buttons.
document.getElementById('profiles-tbody').addEventListener('click', (e) => {
  if (e.target.matches('button.edit-btn')) {
    editProfile(e.target.dataset.name);
  } else if (e.target.matches('button.delete-btn')) {
    deleteProfile(e.target.dataset.name);
  }
});

async function refreshDefaults() {
  const defaults = await get('/api/defaults');
  const tbody = document.getElementById('defaults-tbody');
  clearChildren(tbody);
  if (!defaults || defaults.length === 0) {
    const tr = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 4;
    td.textContent = 'No built-in profiles';
    tr.appendChild(td);
    tbody.appendChild(tr);
    return;
  }
  defaults.forEach(p => {
    const tr = document.createElement('tr');

    const tdName = document.createElement('td');
    tdName.textContent = p.name;

    const tdMatch = document.createElement('td');
    tdMatch.textContent = p.match.type + ': ' + p.match.value;

    const tdPriority = document.createElement('td');
    tdPriority.textContent = String(p.priority);

    const tdAction = document.createElement('td');
    const btn = document.createElement('button');
    btn.className = 'copy-btn';
    btn.textContent = 'Copy to My Profiles';
    btn.dataset.name = p.name;
    tdAction.appendChild(btn);

    tr.appendChild(tdName);
    tr.appendChild(tdMatch);
    tr.appendChild(tdPriority);
    tr.appendChild(tdAction);
    tbody.appendChild(tr);
  });
}

// Event delegation for default profile copy buttons.
document.getElementById('defaults-tbody').addEventListener('click', (e) => {
  if (e.target.matches('button.copy-btn')) {
    copyDefault(e.target.dataset.name);
  }
});

async function refreshAll() {
  await Promise.all([refreshStatus(), refreshProfiles(), refreshDefaults()]);
}

async function toggleAutoDetect() {
  const s = await get('/api/status');
  await put('/api/settings', { auto_detect: !s.auto_detect });
  refreshAll();
}

document.getElementById('override-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const profile = {
    name: document.getElementById('ov-name').value,
    activity: {
      details: document.getElementById('ov-details').value,
      state: document.getElementById('ov-state').value,
      large_image: document.getElementById('ov-large-image').value,
      large_text: document.getElementById('ov-large-text').value,
      small_image: document.getElementById('ov-small-image').value,
      small_text: document.getElementById('ov-small-text').value,
    },
  };
  await post('/api/override', profile);
  refreshAll();
});

async function clearOverride() {
  await del('/api/override');
  refreshAll();
}

function showAddProfile() {
  document.getElementById('modal-title').textContent = 'Add Profile';
  document.getElementById('mf-edit-name').value = '';
  document.getElementById('mf-name').value = '';
  document.getElementById('mf-match-type').value = 'process_name';
  document.getElementById('mf-match-value').value = '';
  document.getElementById('mf-details').value = '';
  document.getElementById('mf-state').value = '';
  document.getElementById('mf-large-image').value = '';
  document.getElementById('mf-large-text').value = '';
  document.getElementById('mf-small-image').value = '';
  document.getElementById('mf-small-text').value = '';
  document.getElementById('mf-priority').value = '5';
  document.getElementById('modal').classList.remove('hidden');
}

async function editProfile(name) {
  const p = await get('/api/profiles/' + encodeURIComponent(name));
  if (!p || !p.name) return;
  document.getElementById('modal-title').textContent = 'Edit Profile: ' + name;
  document.getElementById('mf-edit-name').value = name;
  document.getElementById('mf-name').value = p.name || '';
  document.getElementById('mf-match-type').value = p.match?.type || 'process_name';
  document.getElementById('mf-match-value').value = p.match?.value || '';
  document.getElementById('mf-details').value = p.activity?.details || '';
  document.getElementById('mf-state').value = p.activity?.state || '';
  document.getElementById('mf-large-image').value = p.activity?.large_image || '';
  document.getElementById('mf-large-text').value = p.activity?.large_text || '';
  document.getElementById('mf-small-image').value = p.activity?.small_image || '';
  document.getElementById('mf-small-text').value = p.activity?.small_text || '';
  document.getElementById('mf-priority').value = p.priority || 5;
  document.getElementById('modal').classList.remove('hidden');
}

function closeModal() {
  document.getElementById('modal').classList.add('hidden');
}

document.getElementById('modal-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const editName = document.getElementById('mf-edit-name').value;
  const profile = {
    name: document.getElementById('mf-name').value,
    match: {
      type: document.getElementById('mf-match-type').value,
      value: document.getElementById('mf-match-value').value,
    },
    activity: {
      details: document.getElementById('mf-details').value,
      state: document.getElementById('mf-state').value,
      large_image: document.getElementById('mf-large-image').value,
      large_text: document.getElementById('mf-large-text').value,
      small_image: document.getElementById('mf-small-image').value,
      small_text: document.getElementById('mf-small-text').value,
    },
    priority: parseInt(document.getElementById('mf-priority').value) || 5,
  };

  if (editName) {
    await put('/api/profiles/' + encodeURIComponent(editName), profile);
  } else {
    await post('/api/profiles', profile);
  }
  closeModal();
  refreshAll();
});

async function deleteProfile(name) {
  if (!confirm('Delete profile "' + name + '"?')) return;
  await del('/api/profiles/' + encodeURIComponent(name));
  refreshAll();
}

async function copyDefault(name) {
  const defaults = await get('/api/defaults');
  const p = defaults.find(d => d.name === name);
  if (!p) return;
  p.isDefault = false;
  await post('/api/profiles', p);
  refreshAll();
}

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') closeModal();
});

refreshAll();
setInterval(refreshAll, 3000);
