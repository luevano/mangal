import 'bootstrap/dist/css/bootstrap.min.css'
import 'bootstrap-icons/font/bootstrap-icons.css'
import './assets/main.css'

import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { VueMasonryPlugin } from 'vue-masonry'

import App from './App.vue'
import router from './router'

const app = createApp(App)
app.use(createPinia())
app.use(VueMasonryPlugin)
app.use(router)
app.mount('#app')
