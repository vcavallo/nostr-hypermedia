// Generic Siren hypermedia client
import * as secp256k1 from 'https://esm.sh/@noble/secp256k1@2.1.0';

let currentEntity = null;
let userState = {
  privateKey: null,
  publicKey: null,
  follows: []
};

// Client-side profile cache using localStorage
const PROFILE_CACHE_KEY = 'nostr_profile_cache';
const PROFILE_CACHE_TTL = 10 * 60 * 1000; // 10 minutes in milliseconds

function getProfileCache() {
  try {
    const cached = localStorage.getItem(PROFILE_CACHE_KEY);
    if (!cached) return {};
    return JSON.parse(cached);
  } catch (e) {
    return {};
  }
}

function setProfileCache(cache) {
  try {
    localStorage.setItem(PROFILE_CACHE_KEY, JSON.stringify(cache));
  } catch (e) {
    // localStorage might be full or unavailable
    console.warn('Failed to save profile cache:', e);
  }
}

function getCachedProfile(pubkey) {
  const cache = getProfileCache();
  const entry = cache[pubkey];
  if (!entry) return null;

  // Check TTL
  if (Date.now() - entry.timestamp > PROFILE_CACHE_TTL) {
    // Expired, remove from cache
    delete cache[pubkey];
    setProfileCache(cache);
    return null;
  }

  return entry.profile;
}

function setCachedProfile(pubkey, profile) {
  const cache = getProfileCache();
  cache[pubkey] = {
    profile,
    timestamp: Date.now()
  };
  setProfileCache(cache);
}

// Bech32 decoding (NIP-19)
const BECH32_CHARSET = 'qpzry9x8gf2tvdw0s3jn54khce6mua7l';

function bech32Decode(str) {
  str = str.toLowerCase();
  const pos = str.lastIndexOf('1');
  if (pos < 1 || pos + 7 > str.length) throw new Error('Invalid bech32 string');

  const hrp = str.slice(0, pos);
  const data = str.slice(pos + 1);

  const values = [];
  for (const char of data) {
    const idx = BECH32_CHARSET.indexOf(char);
    if (idx === -1) throw new Error('Invalid character in bech32 string');
    values.push(idx);
  }

  // Remove checksum (last 6 characters)
  const dataValues = values.slice(0, -6);

  // Convert 5-bit groups to 8-bit bytes
  const bytes = convertBits(dataValues, 5, 8, false);

  return { hrp, bytes };
}

function convertBits(data, fromBits, toBits, pad) {
  let acc = 0;
  let bits = 0;
  const result = [];
  const maxv = (1 << toBits) - 1;

  for (const value of data) {
    acc = (acc << fromBits) | value;
    bits += fromBits;
    while (bits >= toBits) {
      bits -= toBits;
      result.push((acc >> bits) & maxv);
    }
  }

  if (pad && bits > 0) {
    result.push((acc << (toBits - bits)) & maxv);
  }

  return new Uint8Array(result);
}

function bytesToHex(bytes) {
  return Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('');
}

function hexToBytes(hex) {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) {
    bytes[i / 2] = parseInt(hex.substr(i, 2), 16);
  }
  return bytes;
}

// Derive public key from private key using secp256k1
async function derivePublicKey(privateKeyHex) {
  const privateKeyBytes = hexToBytes(privateKeyHex);
  const publicKeyBytes = secp256k1.getPublicKey(privateKeyBytes, true);
  // Remove the prefix byte (02 or 03) for x-only pubkey
  return bytesToHex(publicKeyBytes.slice(1));
}

// Login functions
async function doLogin() {
  const nsecInput = document.getElementById('nsec-input');
  const errorEl = document.getElementById('login-error');
  errorEl.style.display = 'none';

  try {
    const nsec = nsecInput.value.trim();
    if (!nsec.startsWith('nsec1')) {
      throw new Error('Invalid nsec format. Must start with nsec1');
    }

    const decoded = bech32Decode(nsec);
    if (decoded.hrp !== 'nsec') {
      throw new Error('Invalid nsec prefix');
    }

    const privateKeyHex = bytesToHex(decoded.bytes);
    if (privateKeyHex.length !== 64) {
      throw new Error('Invalid private key length');
    }

    const publicKeyHex = await derivePublicKey(privateKeyHex);

    userState.privateKey = privateKeyHex;
    userState.publicKey = publicKeyHex;

    // Fetch follows list
    await fetchFollowsList();

    updateUserUI();
    hideLoginModal();
    nsecInput.value = '';

  } catch (error) {
    errorEl.textContent = error.message;
    errorEl.style.display = 'block';
  }
}

function logout() {
  userState.privateKey = null;
  userState.publicKey = null;
  userState.follows = [];
  updateUserUI();
}

function updateUserUI() {
  const loginBtn = document.getElementById('login-btn');
  const followsBtn = document.getElementById('follows-btn');
  const userStatus = document.getElementById('user-status');

  if (userState.publicKey) {
    loginBtn.textContent = 'Logout';
    loginBtn.onclick = logout;
    followsBtn.style.display = 'inline-block';
    userStatus.innerHTML = `<span class="logged-in">Logged in: ${userState.publicKey.substring(0, 12)}... (${userState.follows.length} follows)</span>`;
  } else {
    loginBtn.textContent = 'Login with nsec';
    loginBtn.onclick = showLoginModal;
    followsBtn.style.display = 'none';
    userStatus.innerHTML = '';
  }
}

async function fetchFollowsList() {
  if (!userState.publicKey) return;

  try {
    // Fetch kind 3 (follow list) for the logged-in user
    const response = await fetch(`/timeline?kinds=3&authors=${userState.publicKey}&limit=1`, {
      headers: { 'Accept': 'application/vnd.siren+json' }
    });

    if (!response.ok) throw new Error('Failed to fetch follows');

    const entity = await response.json();

    // Extract p tags from the follow list event
    if (entity.entities && entity.entities.length > 0) {
      const followEvent = entity.entities[0];
      const tags = followEvent.properties?.tags || [];

      userState.follows = tags
        .filter(tag => tag[0] === 'p')
        .map(tag => tag[1]);
    }
  } catch (error) {
    console.error('Failed to fetch follows:', error);
  }
}

function showLoginModal() {
  document.getElementById('login-modal').style.display = 'flex';
  document.getElementById('nsec-input').focus();
}

function hideLoginModal() {
  document.getElementById('login-modal').style.display = 'none';
  document.getElementById('login-error').style.display = 'none';
}

function loadTimeline() {
  navigate('/timeline?kinds=1&limit=20&fast=1');
}

function loadFollowsTimeline() {
  if (userState.follows.length === 0) {
    alert('No follows found. Make sure you are logged in and have a follow list.');
    return;
  }

  // Limit to first 50 follows to avoid URL length issues
  const authorsParam = userState.follows.slice(0, 50).join(',');
  navigate(`/timeline?kinds=1&limit=20&authors=${authorsParam}&fast=1`);
}

// Expose functions to window for onclick handlers
window.showLoginModal = showLoginModal;
window.hideLoginModal = hideLoginModal;
window.doLogin = doLogin;
window.logout = logout;
window.loadTimeline = loadTimeline;
window.loadFollowsTimeline = loadFollowsTimeline;
window.navigate = navigate;

// Navigate to a URL and render the response
async function navigate(url, skipPushState = false) {
  const content = document.getElementById('content');
  content.innerHTML = '<div class="loading">Loading...</div>';

  try {
    const response = await fetch(url, {
      headers: {
        'Accept': 'application/vnd.siren+json'
      }
    });

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    const data = await response.json();
    currentEntity = data;

    // Update browser history
    if (!skipPushState) {
      const displayUrl = urlToDisplayPath(url);
      history.pushState({ url }, '', displayUrl);
    }

    // Check if this is a thread response
    if (isThreadResponse(data)) {
      renderThread(data, content);
    } else {
      renderEntity(data, content);
    }
  } catch (error) {
    content.innerHTML = `<div class="error">Error loading content: ${error.message}</div>`;
  }
}

// The URL IS the state - no transformation needed for hypermedia
// Just use the full URL with query params as the browser URL
function urlToDisplayPath(url) {
  return url;
}

// Convert display path back to API URL
function displayPathToUrl(path) {
  // Handle root path - provide sensible defaults
  if (path === '/' || path === '') {
    return '/timeline?kinds=1&limit=20&fast=1';
  }
  // Otherwise, the URL is the state - use it directly
  return path + window.location.search;
}

// Handle browser back/forward
window.addEventListener('popstate', (event) => {
  if (event.state && event.state.url) {
    navigate(event.state.url, true);
  } else {
    // No state, navigate based on current path
    const url = displayPathToUrl(window.location.pathname);
    navigate(url, true);
  }
});

// Render a Siren entity
function renderEntity(entity, container) {
  container.innerHTML = '';

  const entityDiv = document.createElement('div');
  entityDiv.className = `entity ${entity.class ? entity.class.join(' ') : ''}`;

  // Render properties
  if (entity.properties) {
    const propsDiv = renderProperties(entity.properties);
    entityDiv.appendChild(propsDiv);
  }

  // Render sub-entities (like individual notes)
  if (entity.entities && entity.entities.length > 0) {
    const entitiesDiv = document.createElement('div');
    entitiesDiv.className = 'entities';

    entity.entities.forEach(subEntity => {
      const subDiv = renderSubEntity(subEntity);
      entitiesDiv.appendChild(subDiv);
    });

    entityDiv.appendChild(entitiesDiv);
  }

  // Render links (navigation, pagination)
  if (entity.links && entity.links.length > 0) {
    const linksDiv = renderLinks(entity.links);
    entityDiv.appendChild(linksDiv);
  }

  // Render actions (publish, etc.)
  if (entity.actions && entity.actions.length > 0) {
    const actionsDiv = renderActions(entity.actions);
    entityDiv.appendChild(actionsDiv);
  }

  container.appendChild(entityDiv);
}

// Render properties as key-value pairs
function renderProperties(properties) {
  const propsDiv = document.createElement('div');
  propsDiv.className = 'properties';

  // Special rendering for meta info
  if (properties.queried_relays !== undefined || properties.eose !== undefined) {
    const metaDiv = document.createElement('div');
    metaDiv.className = 'meta-info';

    if (properties.title) {
      const titleEl = document.createElement('h2');
      titleEl.textContent = properties.title;
      titleEl.style.width = '100%';
      titleEl.style.marginBottom = '12px';
      propsDiv.appendChild(titleEl);
    }

    if (properties.queried_relays !== undefined) {
      const item = document.createElement('div');
      item.className = 'meta-item';
      item.innerHTML = `<span class="meta-label">Relays:</span> ${properties.queried_relays}`;
      metaDiv.appendChild(item);
    }

    if (properties.eose !== undefined) {
      const item = document.createElement('div');
      item.className = 'meta-item';
      item.innerHTML = `<span class="meta-label">EOSE:</span> ${properties.eose ? '✓' : '✗'}`;
      metaDiv.appendChild(item);
    }

    if (properties.generated_at) {
      const item = document.createElement('div');
      item.className = 'meta-item';
      const date = new Date(properties.generated_at);
      item.innerHTML = `<span class="meta-label">Generated:</span> ${date.toLocaleTimeString()}`;
      metaDiv.appendChild(item);
    }

    propsDiv.appendChild(metaDiv);
    return propsDiv;
  }

  // Default property rendering
  for (const [key, value] of Object.entries(properties)) {
    if (value === null || value === undefined) continue;

    const propDiv = document.createElement('div');
    propDiv.className = 'property';

    const keyEl = document.createElement('div');
    keyEl.className = 'property-key';
    keyEl.textContent = key.replace(/_/g, ' ');

    const valueEl = document.createElement('div');
    valueEl.className = 'property-value';

    if (typeof value === 'object') {
      valueEl.textContent = JSON.stringify(value, null, 2);
    } else {
      valueEl.textContent = value;
    }

    propDiv.appendChild(keyEl);
    propDiv.appendChild(valueEl);
    propsDiv.appendChild(propDiv);
  }

  return propsDiv;
}

// Render a sub-entity (e.g., a note)
function renderSubEntity(subEntity) {
  const div = document.createElement('div');
  div.className = `sub-entity ${subEntity.class ? subEntity.class.join(' ') : ''}`;

  if (subEntity.properties) {
    // Special rendering for note/event entities
    if (subEntity.class && (subEntity.class.includes('note') || subEntity.class.includes('event'))) {
      const note = renderNote(subEntity.properties);
      div.appendChild(note);
    } else {
      const props = renderProperties(subEntity.properties);
      div.appendChild(props);
    }
  }

  // Render links
  if (subEntity.links && subEntity.links.length > 0) {
    const links = renderLinks(subEntity.links);
    div.appendChild(links);
  }

  // Render actions
  if (subEntity.actions && subEntity.actions.length > 0) {
    const actions = renderActions(subEntity.actions);
    div.appendChild(actions);
  }

  return div;
}

// Extract reply information from tags (NIP-10)
function getReplyInfo(tags) {
  if (!tags || !Array.isArray(tags)) {
    return { isReply: false };
  }

  let root = null;
  let replyTo = null;

  // NIP-10: Look for e tags with markers or positional
  const eTags = tags.filter(t => t[0] === 'e');

  for (const tag of eTags) {
    const marker = tag[3]; // Optional marker (root, reply, mention)
    if (marker === 'root') {
      root = tag[1];
    } else if (marker === 'reply') {
      replyTo = tag[1];
    }
  }

  // If no markers, use positional (first = root, last = reply)
  if (!root && !replyTo && eTags.length > 0) {
    if (eTags.length === 1) {
      replyTo = eTags[0][1];
      root = eTags[0][1];
    } else {
      root = eTags[0][1];
      replyTo = eTags[eTags.length - 1][1];
    }
  }

  return {
    isReply: eTags.length > 0,
    root,
    replyTo
  };
}

// Parse content and render images, links, etc.
function renderContent(content) {
  const container = document.createElement('div');

  // Image URL regex - matches common image extensions
  const imageRegex = /(https?:\/\/[^\s]+\.(?:jpg|jpeg|png|gif|webp|svg|bmp)(?:\?[^\s]*)?)/gi;

  // Split content by image URLs
  const parts = content.split(imageRegex);

  parts.forEach((part, index) => {
    if (imageRegex.test(part)) {
      // Reset regex lastIndex
      imageRegex.lastIndex = 0;

      // This is an image URL - render as img tag
      const img = document.createElement('img');
      img.src = part;
      img.className = 'note-image';
      img.loading = 'lazy';
      img.onclick = () => window.open(part, '_blank');
      img.onerror = () => {
        // If image fails to load, show as link instead
        const link = document.createElement('a');
        link.href = part;
        link.target = '_blank';
        link.textContent = part;
        link.className = 'note-link';
        img.replaceWith(link);
      };
      container.appendChild(img);
    } else if (part) {
      // Regular text - preserve line breaks
      const textSpan = document.createElement('span');
      textSpan.textContent = part;
      container.appendChild(textSpan);
    }
  });

  return container;
}

// Specialized rendering for Nostr notes
function renderNote(props) {
  const noteDiv = document.createElement('div');
  noteDiv.className = 'note';

  // Try to get profile from local cache first, fall back to server-provided
  let profile = props.author_profile;
  if (props.pubkey) {
    const cachedProfile = getCachedProfile(props.pubkey);
    if (cachedProfile) {
      profile = cachedProfile;
    } else if (profile) {
      // Cache the server-provided profile
      setCachedProfile(props.pubkey, profile);
    }
  }

  // Author header with profile info
  const authorDiv = document.createElement('div');
  authorDiv.className = 'note-author';

  // Profile picture (if available)
  if (profile && profile.picture) {
    const avatarImg = document.createElement('img');
    avatarImg.className = 'author-avatar';
    avatarImg.src = profile.picture;
    avatarImg.alt = 'avatar';
    avatarImg.onerror = () => { avatarImg.style.display = 'none'; };
    authorDiv.appendChild(avatarImg);
  }

  // Author name and pubkey
  const authorInfoDiv = document.createElement('div');
  authorInfoDiv.className = 'author-info';

  if (profile && (profile.display_name || profile.name)) {
    const nameSpan = document.createElement('span');
    nameSpan.className = 'author-name';
    nameSpan.textContent = profile.display_name || profile.name;
    authorInfoDiv.appendChild(nameSpan);

    // NIP-05 verification
    if (profile.nip05) {
      const nip05Span = document.createElement('span');
      nip05Span.className = 'author-nip05';
      nip05Span.textContent = profile.nip05;
      authorInfoDiv.appendChild(nip05Span);
    }
  }

  if (props.pubkey) {
    const pubkeySpan = document.createElement('span');
    pubkeySpan.className = 'pubkey';
    pubkeySpan.textContent = props.pubkey.substring(0, 12) + '...';
    pubkeySpan.title = props.pubkey;
    authorInfoDiv.appendChild(pubkeySpan);
  }

  authorDiv.appendChild(authorInfoDiv);
  noteDiv.appendChild(authorDiv);

  // Check if this is a reply (has e tags)
  const replyInfo = getReplyInfo(props.tags);
  if (replyInfo.isReply) {
    const replyDiv = document.createElement('div');
    replyDiv.className = 'note-reply-indicator';
    replyDiv.innerHTML = `<span class="reply-icon">↩</span> Replying to <span class="reply-id">${replyInfo.replyTo.substring(0, 8)}...</span>`;
    replyDiv.onclick = () => navigate(`/thread/${replyInfo.root || replyInfo.replyTo}`);
    replyDiv.style.cursor = 'pointer';
    noteDiv.appendChild(replyDiv);
  }

  // Content - render with images
  if (props.content) {
    const contentDiv = document.createElement('div');
    contentDiv.className = 'note-content';
    const renderedContent = renderContent(props.content);
    contentDiv.appendChild(renderedContent);
    noteDiv.appendChild(contentDiv);
  }

  // Reactions
  if (props.reactions && props.reactions.total > 0) {
    const reactionsDiv = document.createElement('div');
    reactionsDiv.className = 'note-reactions';

    // Show reaction counts by type
    const byType = props.reactions.by_type || {};
    const sortedTypes = Object.entries(byType).sort((a, b) => b[1] - a[1]);

    for (const [type, count] of sortedTypes) {
      const reactionSpan = document.createElement('span');
      reactionSpan.className = 'reaction-badge';
      reactionSpan.textContent = `${type} ${count}`;
      reactionsDiv.appendChild(reactionSpan);
    }

    noteDiv.appendChild(reactionsDiv);
  }

  // Metadata footer
  const metaDiv = document.createElement('div');
  metaDiv.className = 'note-meta';

  if (props.created_at) {
    const timeSpan = document.createElement('span');
    const date = new Date(props.created_at * 1000);
    timeSpan.textContent = date.toLocaleString();
    timeSpan.className = 'timestamp';
    metaDiv.appendChild(timeSpan);
  }

  if (props.relays_seen && props.relays_seen.length > 0) {
    const relaySpan = document.createElement('span');
    relaySpan.textContent = `from ${props.relays_seen.length} relay(s)`;
    relaySpan.title = props.relays_seen.join(', ');
    metaDiv.appendChild(relaySpan);
  }

  // View Thread button - only show if note has replies
  if (props.id && props.reply_count > 0) {
    const threadBtn = document.createElement('button');
    threadBtn.className = 'view-thread-btn';
    threadBtn.textContent = `View Thread (${props.reply_count})`;
    threadBtn.onclick = (e) => {
      e.stopPropagation();
      navigate(`/thread/${props.id}`);
    };
    metaDiv.appendChild(threadBtn);
  }

  noteDiv.appendChild(metaDiv);
  return noteDiv;
}

// Render links
function renderLinks(links) {
  const linksDiv = document.createElement('div');
  linksDiv.className = 'links';

  // Separate pagination from other links
  const paginationLinks = links.filter(l => l.rel.includes('next') || l.rel.includes('prev'));
  const otherLinks = links.filter(l => !l.rel.includes('next') && !l.rel.includes('prev') && !l.rel.includes('self'));

  // Render other links
  otherLinks.forEach(link => {
    const linkEl = document.createElement('a');
    linkEl.className = 'link';
    linkEl.href = '#';
    linkEl.onclick = (e) => {
      e.preventDefault();
      navigate(link.href);
    };

    const relSpan = document.createElement('span');
    relSpan.className = 'link-rel';
    relSpan.textContent = link.rel.join(', ');
    linkEl.appendChild(relSpan);

    linkEl.appendChild(document.createTextNode(' →'));

    linksDiv.appendChild(linkEl);
  });

  // Render pagination separately
  if (paginationLinks.length > 0) {
    const paginationDiv = document.createElement('div');
    paginationDiv.className = 'pagination';

    paginationLinks.forEach(link => {
      const linkEl = document.createElement('a');
      linkEl.className = 'link';
      linkEl.href = '#';
      linkEl.textContent = link.rel.includes('next') ? 'Next Page →' : '← Previous Page';
      linkEl.onclick = (e) => {
        e.preventDefault();
        navigate(link.href);
      };
      paginationDiv.appendChild(linkEl);
    });

    linksDiv.appendChild(paginationDiv);
  }

  return linksDiv;
}

// Render actions as forms
function renderActions(actions) {
  const actionsDiv = document.createElement('div');
  actionsDiv.className = 'actions';

  actions.forEach(action => {
    const form = document.createElement('form');
    form.className = 'action-form';
    form.onsubmit = (e) => {
      e.preventDefault();
      executeAction(action, form);
    };

    // Toggle button
    const toggleBtn = document.createElement('button');
    toggleBtn.type = 'button';
    toggleBtn.className = 'action-toggle';
    toggleBtn.textContent = action.title || action.name;

    const fieldsDiv = document.createElement('div');
    fieldsDiv.className = 'action-fields';

    toggleBtn.onclick = () => {
      fieldsDiv.classList.toggle('visible');
    };

    // Render fields
    if (action.fields && action.fields.length > 0) {
      action.fields.forEach(field => {
        const fieldDiv = document.createElement('div');
        fieldDiv.className = 'action-field';

        const label = document.createElement('label');
        label.textContent = field.title || field.name;
        label.htmlFor = `field-${action.name}-${field.name}`;
        fieldDiv.appendChild(label);

        let input;
        if (field.type === 'textarea' || (field.name === 'content' && !field.type)) {
          input = document.createElement('textarea');
        } else {
          input = document.createElement('input');
          input.type = field.type || 'text';
        }

        input.name = field.name;
        input.id = `field-${action.name}-${field.name}`;
        if (field.value !== undefined && field.value !== null) {
          input.value = field.value;
        }

        fieldDiv.appendChild(input);
        fieldsDiv.appendChild(fieldDiv);
      });

      // Submit button
      const submitBtn = document.createElement('button');
      submitBtn.type = 'submit';
      submitBtn.className = 'action-submit';
      submitBtn.textContent = `Submit ${action.name}`;
      fieldsDiv.appendChild(submitBtn);
    }

    form.appendChild(toggleBtn);
    form.appendChild(fieldsDiv);
    actionsDiv.appendChild(form);
  });

  return actionsDiv;
}

// Execute a Siren action
async function executeAction(action, form) {
  const formData = new FormData(form);
  const data = {};

  for (const [key, value] of formData.entries()) {
    data[key] = value;
  }

  try {
    const response = await fetch(action.href, {
      method: action.method || 'POST',
      headers: {
        'Content-Type': action.type || 'application/json',
      },
      body: JSON.stringify(data)
    });

    if (response.ok) {
      alert(`Action "${action.name}" completed successfully!`);
      // Optionally refresh the current view
      if (currentEntity && currentEntity.links) {
        const selfLink = currentEntity.links.find(l => l.rel.includes('self'));
        if (selfLink) {
          navigate(selfLink.href);
        }
      }
    } else {
      const error = await response.text();
      alert(`Action failed: ${response.status} ${error}`);
    }
  } catch (error) {
    alert(`Action failed: ${error.message}`);
  }
}

// Check if the response is a thread or timeline and render appropriately
function isThreadResponse(data) {
  return data.root !== undefined && data.replies !== undefined;
}

// Render a thread view
function renderThread(threadData, container) {
  container.innerHTML = '';

  const threadDiv = document.createElement('div');
  threadDiv.className = 'thread-view';

  // Back button
  const backBtn = document.createElement('button');
  backBtn.className = 'back-btn';
  backBtn.textContent = '← Back to Timeline';
  backBtn.onclick = () => navigate('/timeline?kinds=1&limit=20&fast=1');
  threadDiv.appendChild(backBtn);

  // Thread header
  const header = document.createElement('h2');
  header.className = 'thread-header';
  header.textContent = `Thread (${threadData.replies.length} replies)`;
  threadDiv.appendChild(header);

  // Root note (highlighted)
  const rootDiv = document.createElement('div');
  rootDiv.className = 'thread-root';
  const rootNote = renderNote(threadData.root);
  rootDiv.appendChild(rootNote);
  threadDiv.appendChild(rootDiv);

  // Replies
  if (threadData.replies.length > 0) {
    const repliesHeader = document.createElement('h3');
    repliesHeader.className = 'replies-header';
    repliesHeader.textContent = 'Replies';
    threadDiv.appendChild(repliesHeader);

    const repliesDiv = document.createElement('div');
    repliesDiv.className = 'thread-replies';

    threadData.replies.forEach(reply => {
      const replyDiv = document.createElement('div');
      replyDiv.className = 'thread-reply';
      const replyNote = renderNote(reply);
      replyDiv.appendChild(replyNote);
      repliesDiv.appendChild(replyDiv);
    });

    threadDiv.appendChild(repliesDiv);
  }

  container.appendChild(threadDiv);
}

// Initialize based on current URL (path + query string)
window.onload = () => {
  // The full URL (path + search) IS the state
  const fullUrl = window.location.pathname + window.location.search;

  // If just root, redirect to default timeline
  let url = fullUrl;
  if (fullUrl === '/' || fullUrl === '') {
    url = '/timeline?kinds=1&limit=20&fast=1';
    // Update URL to show the actual state
    history.replaceState({ url }, '', url);
  } else {
    // Keep current URL, just store in state
    history.replaceState({ url }, '', fullUrl);
  }

  navigate(url, true); // Skip pushState since we just did replaceState
};

// Load timeline with full enrichment (profiles and reactions) - slower
function loadTimelineFull() {
  navigate('/timeline?kinds=1&limit=10');
}
window.loadTimelineFull = loadTimelineFull;
