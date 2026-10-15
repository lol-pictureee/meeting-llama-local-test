// Meeting Summarizer — Frontend JavaScript
const API = '/api';
let currentFile = null;
let pollingInterval = null;

document.addEventListener('DOMContentLoaded', () => {
    const dropZone = document.getElementById('drop-zone');
    const fileInput = document.getElementById('file-input');
    const uploadForm = document.getElementById('upload-form');
    const refreshBtn = document.getElementById('refresh-btn');
    const closeModalBtn = document.getElementById('close-modal');
    const modalOverlay = document.getElementById('modal-overlay');

    // Connection check
    checkConnection();
    setInterval(checkConnection, 30000);

    // Drag and drop
    dropZone.addEventListener('click', () => fileInput.click());
    dropZone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropZone.classList.add('dragover');
    });
    dropZone.addEventListener('dragleave', () => dropZone.classList.remove('dragover'));
    dropZone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropZone.classList.remove('dragover');
        if (e.dataTransfer.files.length > 0) {
            handleFileSelect(e.dataTransfer.files[0]);
        }
    });

    // File input change
    fileInput.addEventListener('change', () => {
        if (fileInput.files.length > 0) {
            handleFileSelect(fileInput.files[0]);
        }
    });

    // Upload form submission
    uploadForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        if (!currentFile) return;

        await uploadFile(currentFile);
    });

    // Refresh button
    refreshBtn.addEventListener('click', loadMeetings);

    // Close modal
    closeModalBtn.addEventListener('click', closeModal);
    modalOverlay.addEventListener('click', (e) => {
        if (e.target === modalOverlay) closeModal();
    });

    // Initial load
    loadMeetings();
});

async function checkConnection() {
    try {
        const resp = await fetch(`${API}/health`);
        if (resp.ok) {
            updateConnectionStatus(true);
        } else {
            updateConnectionStatus(false);
        }
    } catch {
        updateConnectionStatus(false);
    }
}

function updateConnectionStatus(connected) {
    const indicator = document.getElementById('connection-status');
    const text = document.getElementById('connection-text');
    indicator.className = `status-indicator ${connected ? 'connected' : 'disconnected'}`;
    text.textContent = connected ? 'Сервер подключен' : 'Нет подключения к серверу';
}

function handleFileSelect(file) {
    currentFile = file;
    const dropText = document.getElementById('drop-text');
    const uploadBtn = document.getElementById('upload-btn');
    const sizeKB = (file.size / 1024).toFixed(1);
    const sizeMB = (file.size / (1024 * 1024)).toFixed(1);
    const sizeStr = file.size > 1024 * 1024 ? `${sizeMB} МБ` : `${sizeKB} КБ`;
    dropText.textContent = `📎 ${file.name} (${sizeStr})`;
    uploadBtn.disabled = false;
}

async function uploadFile(file) {
    const formData = new FormData();
    formData.append('file', file);

    const progress = document.getElementById('upload-progress');
    const uploadBtn = document.getElementById('upload-btn');
    progress.style.display = 'block';
    uploadBtn.disabled = true;

    try {
        const xhr = new XMLHttpRequest();
        xhr.open('POST', `${API}/meetings/upload`);

        xhr.upload.onprogress = (e) => {
            if (e.lengthComputable) {
                const pct = Math.round((e.loaded / e.total) * 100);
                progress.style.width = `${pct}%`;
            }
        };

        xhr.onload = () => {
            progress.style.display = 'none';
            progress.style.width = '';
            if (xhr.status >= 200 && xhr.status < 300) {
                const data = JSON.parse(xhr.responseText);
                currentFile = null;
                document.getElementById('file-input').value = '';
                document.getElementById('drop-text').textContent = 'Перетащите файл сюда или нажмите для выбора';
                uploadBtn.disabled = true;
                startPolling(data.id);
                loadMeetings();
            } else {
                alert(`Ошибка загрузки: ${xhr.responseText}`);
                uploadBtn.disabled = false;
            }
        };

        xhr.onerror = () => {
            progress.style.display = 'none';
            alert('Ошибка сети при загрузке файла');
            uploadBtn.disabled = false;
        };

        xhr.send(formData);
    } catch (err) {
        progress.style.display = 'none';
        alert(`Ошибка: ${err.message}`);
        uploadBtn.disabled = false;
    }
}

function startPolling(meetingId) {
    if (pollingInterval) clearInterval(pollingInterval);
    pollingInterval = setInterval(async () => {
        try {
            const resp = await fetch(`${API}/meetings/${meetingId}`);
            if (resp.ok) {
                const data = await resp.json();
                const status = data.meeting?.status;
                if (status === 'completed' || status === 'error') {
                    clearInterval(pollingInterval);
                    pollingInterval = null;
                    loadMeetings();
                }
            }
        } catch {}
    }, 3000);
}

async function loadMeetings() {
    try {
        const resp = await fetch(`${API}/meetings`);
        if (!resp.ok) throw new Error('Failed to load meetings');
        const meetings = await resp.json();
        renderMeetings(meetings);
    } catch (err) {
        const tbody = document.getElementById('meetings-list');
        tbody.innerHTML = `<tr><td colspan="6" class="empty-row">Не удалось загрузить список встреч</td></tr>`;
    }
}

function renderMeetings(meetings) {
    const tbody = document.getElementById('meetings-list');

    if (meetings.length === 0) {
        tbody.innerHTML = `<tr><td colspan="6" class="empty-row">Пока нет загруженных встреч. Загрузите первый аудиофайл!</td></tr>`;
        return;
    }

    tbody.innerHTML = meetings.map(m => {
        const date = new Date(m.created_at).toLocaleString('ru-RU');
        const sizeStr = m.size_bytes > 1024 * 1024
            ? `${(m.size_bytes / (1024 * 1024)).toFixed(1)} MB`
            : `${(m.size_bytes / 1024).toFixed(1)} KB`;

        return `
            <tr>
                <td>${m.id}</td>
                <td title="${escapeHtml(m.original_name)}">${truncate(m.original_name, 30)}</td>
                <td>${sizeStr}</td>
                <td><span class="badge ${escapeHtml(m.status)}">${formatStatus(m.status)}</span></td>
                <td>${date}</td>
                <td>
                    <button class="action-btn" onclick="viewMeeting(${m.id})">👁 Просмотр</button>
                    ${m.status === 'completed' ? `<button class="action-btn" onclick="exportSummary(${m.id})">📥 Экспорт</button>` : ''}
                    ${(m.status === 'error' || m.status === 'loaded') ? `<button class="action-btn" onclick="reprocessMeeting(${m.id})">🔁 Обработать</button>` : ''}
                </td>
            </tr>
        `;
    }).join('');
}

async function reprocessMeeting(id) {
    if (!confirm('Запустить обработку этого файла заново?')) return;
    try {
        const resp = await fetch(`${API}/meetings/${id}/reprocess`, { method: 'POST' });
        if (!resp.ok) {
            const err = await resp.json().catch(() => ({ error: resp.statusText }));
            alert(`Не удалось запустить обработку: ${err.error}`);
            return;
        }
        startPolling(id);
        loadMeetings();
    } catch (err) {
        alert(`Ошибка сети: ${err.message}`);
    }
}

async function viewMeeting(id) {
    const overlay = document.getElementById('modal-overlay');
    const body = document.getElementById('modal-body');
    body.innerHTML = '<div class="modal-loading">Загрузка данных...</div>';
    overlay.style.display = 'flex';

    try {
        const resp = await fetch(`${API}/meetings/${id}`);
        if (!resp.ok) throw new Error('Meeting not found');
        const data = await resp.json();
        renderMeetingDetail(data, id);
    } catch (err) {
        body.innerHTML = `<p style="color:var(--error)">Ошибка: ${escapeHtml(err.message)}</p>`;
    }
}

function renderMeetingDetail(data, id) {
    const body = document.getElementById('modal-body');
    const meeting = data.meeting;
    const segments = data.segments || [];
    const summary = data.summary || null;

    let html = '';

    // Meeting info
    html += `
        <div class="summary-section">
            <h4>Информация</h4>
            <p><strong>Имя:</strong> ${escapeHtml(meeting.original_name)}</p>
            <p><strong>Статус:</strong> <span class="badge ${escapeHtml(meeting.status)}">${formatStatus(meeting.status)}</span></p>
            ${meeting.error_message ? `<p><strong style="color:var(--error)">Ошибка:</strong> ${escapeHtml(meeting.error_message)}</p>` : ''}
        </div>
    `;

    // Summary
    if (summary) {
        if (summary.summary) {
            html += `<div class="summary-section"><h4>📋 Краткое содержание</h4><p>${escapeHtml(summary.summary)}</p></div>`;
        }
        if (summary.decisions && summary.decisions.length > 0) {
            html += `<div class="summary-section"><h4>✅ Решения</h4><ul>${summary.decisions.map(d => `<li>${escapeHtml(d)}</li>`).join('')}</ul></div>`;
        }
        if (summary.tasks && summary.tasks.length > 0) {
            html += `<div class="summary-section"><h4>📌 Задачи</h4><ul>${summary.tasks.map(t => {
                let text = escapeHtml(t.text);
                if (t.assigned_to) text += ` — <strong>${escapeHtml(t.assigned_to)}</strong>`;
                if (t.due_date) text += ` (до: ${escapeHtml(t.due_date)})`;
                return `<li>${text}</li>`;
            }).join('')}</ul></div>`;
        }
        if (summary.risks && summary.risks.length > 0) {
            html += `<div class="summary-section"><h4>⚠️ Риски</h4><ul>${summary.risks.map(r => `<li>${escapeHtml(r)}</li>`).join('')}</ul></div>`;
        }
        if (summary.open_questions && summary.open_questions.length > 0) {
            html += `<div class="summary-section"><h4>❓ Открытые вопросы</h4><ul>${summary.open_questions.map(q => `<li>${escapeHtml(q)}</li>`).join('')}</ul></div>`;
        }
        if (summary.quotes && summary.quotes.length > 0) {
            html += `<div class="summary-section"><h4>💬 Цитаты</h4><ul>${summary.quotes.map(q => {
                let text = escapeHtml(q.text);
                if (q.start_time !== undefined) text += ` [${q.start_time}s]`;
                return `<li>${text}</li>`;
            }).join('')}</ul></div>`;
        }

        html += `<div class="export-links"><a href="${API}/meetings/${id}/export?format=json">Скачать JSON</a> <a href="${API}/meetings/${id}/export?format=markdown">Скачать Markdown</a></div>`;
    }

    // Transcript
    if (segments.length > 0) {
        html += `
            <div class="summary-section">
                <h4>🎙 Транскрипт (${segments.length} сегментов)</h4>
                <div class="transcript-text">${renderTranscriptSegments(segments)}</div>
            </div>
        `;
    }

    body.innerHTML = html;
}

function renderTranscriptSegments(segments) {
    return segments.map(s => {
        const time = `[${s.start_time.toFixed(1)} — ${s.end_time.toFixed(1)}]`;
        const speaker = s.speaker ? ` **${escapeHtml(s.speaker)}** ` : '';
        return `${time}${speaker}${escapeHtml(s.text)}\n`;
    }).join('\n');
}

function exportSummary(id) {
    window.open(`${API}/meetings/${id}/export?format=markdown`, '_blank');
}

function closeModal() {
    document.getElementById('modal-overlay').style.display = 'none';
}

// Helpers
function formatStatus(status) {
    const map = {
        loaded: 'Загружено',
        transcribing: 'Распознаётся',
        analyzing: 'Анализируется',
        completed: 'Готово',
        error: 'Ошибка'
    };
    return map[status] || status;
}

function escapeHtml(str) {
    if (!str) return '';
    return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;');
}

function truncate(str, max) {
    if (!str || str.length <= max) return str || '';
    return str.substring(0, max) + '...';
}
