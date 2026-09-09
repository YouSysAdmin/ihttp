import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
// Self-hosted fonts, because the console ships inside a Go binary and
// may run with no way out to a font CDN. The wght entry carries the
// upright faces only.
import '@fontsource-variable/geist/wght.css'
import '@fontsource-variable/geist-mono/wght.css'
import './assets/styles.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)

app.mount('#app')
