import { useState } from 'react';

const services = [
  { name: 'Spotify', key: 'spotify', detail: 'Library connected', tone: 'green', mark: 'S' },
  { name: 'Apple Music', key: 'apple-music', detail: 'Library connected', tone: 'red', mark: '' },
];

const activity = [
  { title: 'Late night drive', detail: 'Spotify → Apple Music', time: 'Today, 09:42', count: '42 tracks', tone: 'violet' },
  { title: 'Focus / Flow', detail: 'Apple Music → Spotify', time: 'Yesterday, 18:10', count: '28 tracks', tone: 'blue' },
  { title: 'Sunday reset', detail: 'Spotify → Apple Music', time: 'Mar 18, 12:32', count: '16 tracks', tone: 'orange' },
];

function Home() {
  const [source, setSource] = useState('spotify');
  const [destination, setDestination] = useState('apple-music');
  const [activeView, setActiveView] = useState('Overview');

  const swapServices = () => {
    setSource(destination);
    setDestination(source);
  };

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <a className="brand" href="#top" aria-label="Resonance home">
          <span className="brand-mark"><span /></span>
          <span>resonance</span>
        </a>

        <div className="sidebar-label">Workspace</div>
        <nav className="nav-list" aria-label="Main navigation">
          {['Overview', 'Transfers', 'Library', 'Insights'].map((item) => (
            <button className={`nav-item ${activeView === item ? 'is-active' : ''}`} key={item} onClick={() => setActiveView(item)}>
              <NavIcon name={item} />
              <span>{item}</span>
              {item === 'Transfers' && <span className="nav-count">2</span>}
            </button>
          ))}
        </nav>

        <div className="sidebar-label services-label">Services</div>
        <div className="service-links">
          {services.map((service) => (
            <div className="service-link" key={service.key}>
              <span className={`service-dot ${service.tone}`}>{service.mark}</span>
              <span>{service.name}</span>
              <span className="online-dot" />
            </div>
          ))}
        </div>

        <div className="sidebar-footer">
          <button className="user-card">
            <span className="avatar">PA</span>
            <span className="user-copy"><strong>Pablo Alcolea</strong><small>Personal workspace</small></span>
            <span className="more">•••</span>
          </button>
          <span className="version">Resonance 0.1.0</span>
        </div>
      </aside>

      <main className="main-content" id="top">
        <header className="topbar">
          <a className="topbar-brand" href="#top" aria-label="Resonance home"><span className="brand-mark"><span /></span><span>RESONANCE</span></a>
          <nav className="top-nav" aria-label="Main navigation">
            {['Overview', 'Transfers', 'Library', 'Insights'].map((item) => <button key={item} className={activeView === item ? 'is-active' : ''} onClick={() => setActiveView(item)}>{item}</button>)}
          </nav>
          <div className="topbar-actions">
            <button className="icon-button" aria-label="Notifications"><BellIcon /><span className="notification-dot" /></button>
            <button className="help-button">?</button>
          </div>
        </header>

        <div className="content-wrap">
          <section className="hero-row">
            <div>
              <p className="eyebrow">PERSONAL MUSIC TRANSFER SYSTEM · 10 SEP 2026</p>
              <h1>YOUR LISTENING<br />WORLD, IN SYNC.</h1>
              <p className="hero-copy">Move playlists between services with confidence.</p>
            </div>
            <button className="primary-button" onClick={() => document.getElementById('transfer-builder')?.scrollIntoView({ behavior: 'smooth' })}>START A TRANSFER <span>↗</span></button>
          </section>

          <section className="stats-grid" aria-label="Library statistics">
            <StatCard label="Tracks in library" value="2,846" change="+12.4%" detail="vs last month" tone="violet" icon={<MusicIcon />} />
            <StatCard label="Saved playlists" value="38" change="+3" detail="this month" tone="blue" icon={<PlaylistIcon />} />
            <StatCard label="Listening minutes" value="12.8k" change="+8.2%" detail="vs last month" tone="orange" icon={<WaveIcon />} />
          </section>

          <section className="dashboard-grid">
            <div className="panel transfer-panel" id="transfer-builder">
              <div className="panel-heading">
                <div><p className="eyebrow">Move your music</p><h2>Start a transfer</h2></div>
                <span className="sparkle">✦</span>
              </div>
              <p className="panel-copy">Move a playlist between your connected services. We’ll preview every match before anything changes.</p>

              <div className="transfer-builder">
                <ServiceSelect label="From" value={source} onChange={setSource} exclude={destination} />
                <button className="swap-button" aria-label="Swap source and destination" onClick={swapServices}><SwapIcon /></button>
                <ServiceSelect label="To" value={destination} onChange={setDestination} exclude={source} />
              </div>
              <div className="select-row"><label>Playlist</label><button className="playlist-select"><span className="playlist-art"><WaveIcon /></span><span><strong>Late night drive</strong><small>42 tracks · Updated today</small></span><ChevronIcon /></button></div>
              <button className="wide-button">Preview transfer <ArrowIcon /></button>
            </div>

            <div className="panel connections-panel">
              <div className="panel-heading"><div><p className="eyebrow">Your ecosystem</p><h2>Connected services</h2></div><button className="text-button">Manage <ArrowIcon /></button></div>
              <div className="connected-list">
                {services.map((service) => <ConnectedService key={service.key} service={service} />)}
              </div>
              <button className="secondary-button">＋ Connect another service</button>
              <div className="privacy-note"><LockIcon /><span>Your credentials are encrypted and never shared.</span></div>
            </div>
          </section>

          <section className="activity-section">
            <div className="section-heading"><div><p className="eyebrow">Keep track</p><h2>Recent activity</h2></div><button className="text-button">View all <ArrowIcon /></button></div>
            <div className="activity-table">
              <div className="activity-header"><span>TRANSFER</span><span>STATUS</span><span>WHEN</span><span>TRACKS</span><span /></div>
              {activity.map((item) => <ActivityRow key={item.title} item={item} />)}
            </div>
          </section>
        </div>
      </main>
    </div>
  );
}

function StatCard({ label, value, change, detail, tone, icon }) {
  return <article className="stat-card"><span className={`stat-icon ${tone}`}>{icon}</span><div className="stat-copy"><p>{label}</p><strong>{value}</strong><small><span>{change}</span> {detail}</small></div><span className="stat-arrow">↗</span></article>;
}

function ServiceSelect({ label, value, onChange, exclude }) {
  const service = services.find((item) => item.key === value);
  return <label className="service-select"><span>{label}</span><div className="select-control"><span className={`service-dot ${service.tone}`}>{service.mark}</span><select value={value} onChange={(event) => onChange(event.target.value)}>{services.filter((item) => item.key !== exclude || item.key === value).map((item) => <option value={item.key} key={item.key}>{item.name}</option>)}</select><ChevronIcon /></div></label>;
}

function ConnectedService({ service }) {
  return <div className="connected-service"><span className={`service-dot large ${service.tone}`}>{service.mark}</span><span className="connected-copy"><strong>{service.name}</strong><small>{service.detail}</small></span><span className="connected-status"><span className="online-dot" /> Connected</span><button className="more" aria-label={`More options for ${service.name}`}>•••</button></div>;
}

function ActivityRow({ item }) {
  return <div className="activity-row"><span className="activity-name"><span className={`activity-art ${item.tone}`}><WaveIcon /></span><span><strong>{item.title}</strong><small>{item.detail}</small></span></span><span className="status-pill"><span className="online-dot" /> Completed</span><span className="activity-time">{item.time}</span><span className="activity-count">{item.count}</span><button className="more" aria-label={`More options for ${item.title}`}>•••</button></div>;
}

function NavIcon({ name }) {
  const paths = { Overview: 'M4 13h6V4H4v9Zm10 7h6v-9h-6v9ZM4 20h6v-3H4v3Zm10-9h6V4h-6v7Z', Transfers: 'm7 7 5-4 5 4m-5-4v14m5-4-5 4-5-4', Library: 'M4 5.5A2.5 2.5 0 0 1 6.5 3H20v15H6.5A2.5 2.5 0 0 0 4 20.5v-15Z', Insights: 'M4 19V9m5 10V5m5 14v-7m5 7V3' };
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d={paths[name]} /></svg>;
}
function MusicIcon() { return <svg viewBox="0 0 24 24"><path d="M9 18V5l10-2v13M9 18a3 3 0 1 1-3-3 3 3 0 0 1 3 3Zm10-2a3 3 0 1 1-3-3 3 3 0 0 1 3 3Z" /></svg>; }
function PlaylistIcon() { return <svg viewBox="0 0 24 24"><path d="M4 6h11M4 11h11M4 16h7m7-7v11m0-11 4-1v8m-4 4a2 2 0 1 1-2-2 2 2 0 0 1 2 2Zm4-4a2 2 0 1 1-2-2 2 2 0 0 1 2 2Z" /></svg>; }
function WaveIcon() { return <svg viewBox="0 0 24 24"><path d="M3 12h2l2-6 4 12 3-9 2 5h5" /></svg>; }
function SwapIcon() { return <svg viewBox="0 0 24 24"><path d="M17 3l4 4-4 4M3 7h18M7 21l-4-4 4-4m14 4H3" /></svg>; }
function ArrowIcon() { return <svg viewBox="0 0 24 24"><path d="M5 12h13m-5-5 5 5-5 5" /></svg>; }
function ChevronIcon() { return <svg viewBox="0 0 24 24"><path d="m7 9 5 5 5-5" /></svg>; }
function BellIcon() { return <svg viewBox="0 0 24 24"><path d="M18 9a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9Zm-8.5 13h5" /></svg>; }
function LockIcon() { return <svg viewBox="0 0 24 24"><rect x="5" y="10" width="14" height="10" rx="2" /><path d="M8 10V7a4 4 0 0 1 8 0v3" /></svg>; }

export default Home;
