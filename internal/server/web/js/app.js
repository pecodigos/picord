async function refreshStatus() {
  const s = await get('/api/status');
  document.getElementById('active-name').textContent = s.active_name || 'None';
  document.getElementById('active-proc').textContent = s.active_process || '-';
  document.getElementById('auto-detect-status').textContent = s.auto_detect ? 'On' : 'Off';
  document.getElementById('override-status').textContent = s.has_override ? 'Yes' : 'No';

  const btn = document.getElementById('btn-auto-detect');
  btn.textContent = s.auto_detect ? 'Disable Auto-Detect' : 'Enable Auto-Detect';

  const tbody = document.getElementById('process-tbody');
  if (!s.detected_processes || s.detected_processes.length === 0) {
    tbody.innerHTML = '<tr><td colspan="3">No processes detected</td></tr>';
  } else {
    tbody.innerHTML = s.detected_processes.map(p =>
      `<tr><td>${p.PID}</td><td>${p.Name}</td><td>${p.window_title || '-'}</td></tr>`
    ).join('');
  }
}

async function refreshProfiles() {
  const profiles = await get('/api/profiles');
  const tbody = document.getElementById('profiles-tbody');
  if (!profiles || profiles.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5">No custom profiles yet</td></tr>';
    return;
  }
  tbody.innerHTML = profiles.map(p =>
    `<tr>
      <td>${p.name}</td>
      <td>${p.match.type}: ${p.match.value}</td>
      <td>${p.priority}</td>
      <td><span class="badge ${p.enabled ? 'badge-on' : 'badge-off'}">${p.enabled ? 'On' : 'Off'}</span></td>
      <td class="actions-cell">
        <button class="edit-btn" onclick="editProfile('${p.name}')">Edit</button>
        <button class="delete-btn" onclick="deleteProfile('${p.name}')">Del</button>
      </td>
    </tr>`
  ).join('');
}

async function refreshDefaults() {
  const defaults = await get('/api/defaults');
  const tbody = document.getElementById('defaults-tbody');
  if (!defaults || defaults.length === 0) {
    tbody.innerHTML = '<tr><td colspan="4">No built-in profiles</td></tr>';
    return;
  }
  tbody.innerHTML = defaults.map(p =>
    `<tr>
      <td>${p.name}</td>
      <td>${p.match.type}: ${p.match.value}</td>
      <td>${p.priority}</td>
      <td><button class="copy-btn" onclick="copyDefault('${p.name}')">Copy to My Profiles</button></td>
    </tr>`
  ).join('');
}

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
