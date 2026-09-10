import {
  Links,
  Meta,
  Outlet,
  Scripts,
  ScrollRestoration,
} from 'react-router';

import stylesheet from '../src/styles.css?url';

export const links = () => [{ rel: 'stylesheet', href: stylesheet }];

export const meta = () => [
  { title: 'Resonance — Music transfer workspace' },
  { name: 'description', content: 'Move your music between services with confidence.' },
];

export function Layout({ children }) {
  return (
    <html lang="en">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="theme-color" content="#0b0c12" />
        <Meta />
        <Links />
      </head>
      <body>
        {children}
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  );
}

export default function Root() {
  return <Outlet />;
}
