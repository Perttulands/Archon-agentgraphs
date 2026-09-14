import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { ThemeProvider } from './theme/ThemeContext'
import { fontsReady } from './theme/fonts'
import './styles/formations-d7.css'
import './styles/agents.css'
import './styles/standalone.css'

void fontsReady().then(() => {
  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode><ThemeProvider><App /></ThemeProvider></React.StrictMode>,
  )
}, () => {
  document.getElementById('root')!.textContent = 'JetBrains Mono could not load. Reload to try again.'
})
