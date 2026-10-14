import { createApp } from 'vue'
import 'bootstrap/dist/css/bootstrap.min.css'
import 'bootstrap-icons/font/bootstrap-icons.min.css'
import '../style.css'
import App from './App.vue'
import { applyTheme } from '../theme'

applyTheme()
createApp(App).mount('#app')
