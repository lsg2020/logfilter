import { createRouter, createWebHashHistory } from 'vue-router'
import ClientConfigure from '../components/ClientConfigure.vue'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/configure',
      name: 'ClientConfigure',
      component: ClientConfigure,
    },
  ],
})

export default router
