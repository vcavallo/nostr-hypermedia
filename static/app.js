// Generic Siren hypermedia client

let currentEntity = null;

// Navigate to a URL and render the Siren response
async function navigate(url) {
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

    const entity = await response.json();
    currentEntity = entity;
    renderEntity(entity, content);
  } catch (error) {
    content.innerHTML = `<div class="error">Error loading content: ${error.message}</div>`;
  }
}

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

// Specialized rendering for Nostr notes
function renderNote(props) {
  const noteDiv = document.createElement('div');
  noteDiv.className = 'note';

  // Content
  if (props.content) {
    const contentDiv = document.createElement('div');
    contentDiv.className = 'note-content';
    contentDiv.textContent = props.content;
    noteDiv.appendChild(contentDiv);
  }

  // Metadata
  const metaDiv = document.createElement('div');
  metaDiv.className = 'note-meta';

  if (props.pubkey) {
    const pubkeySpan = document.createElement('span');
    pubkeySpan.className = 'pubkey';
    pubkeySpan.textContent = props.pubkey.substring(0, 16) + '...';
    pubkeySpan.title = props.pubkey;
    metaDiv.appendChild(pubkeySpan);
  }

  if (props.created_at) {
    const timeSpan = document.createElement('span');
    const date = new Date(props.created_at * 1000);
    timeSpan.textContent = date.toLocaleString();
    timeSpan.className = 'timestamp';
    metaDiv.appendChild(timeSpan);
  }

  if (props.kind !== undefined) {
    const kindSpan = document.createElement('span');
    kindSpan.textContent = `kind: ${props.kind}`;
    metaDiv.appendChild(kindSpan);
  }

  if (props.relays_seen && props.relays_seen.length > 0) {
    const relaySpan = document.createElement('span');
    relaySpan.textContent = `from ${props.relays_seen.length} relay(s)`;
    relaySpan.title = props.relays_seen.join(', ');
    metaDiv.appendChild(relaySpan);
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

// Initialize by loading the default timeline
window.onload = () => {
  navigate('/timeline?kinds=1&limit=20');
};
