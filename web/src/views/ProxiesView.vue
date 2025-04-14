<template>
  <div class="px-4 sm:px-6 lg:px-8">
    <div class="sm:flex sm:items-center">
      <div class="sm:flex-auto">
        <h1 class="text-base font-semibold leading-6 text-gray-900">Proxies</h1>
        <p class="mt-2 text-sm text-gray-700">A list of all proxies in your A/B testing system.</p>
      </div>
      <div class="mt-4 sm:ml-16 sm:mt-0 sm:flex-none">
        <button
            @click="openCreateModal"
            class="block rounded-md bg-indigo-600 px-3 py-2 text-center text-sm font-semibold text-white shadow-sm hover:bg-indigo-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
        >
          Add proxy
        </button>
      </div>
    </div>

    <!-- Filter by tags -->
    <div class="mt-4">
      <TagsInput
          v-model="selectedTags"
          label="Filter by tags"
          :available-tags="availableTags"
          @update:modelValue="filterProxies"
      />
    </div>

    <!-- Proxies List -->
    <ProxiesList :filteredProxies="filteredProxies" :sortBy="sortBy" :sortDesc="sortDesc" @sort="handleSort"
                 @delete="deleteProxy" @edit="editProxy" @viewHistory="viewHistory"/>

    <!-- Pagination -->
    <Pagination :currentPage="currentPage" :itemsPerPage="itemsPerPage" @changePage="changePage"
                :totalProxies="totalProxies"/>

    <!-- Create/Edit Modal -->
    <div v-if="showModal" class="relative z-10">
      <EditModal :proxy="editingProxy" :form="form" :available-tags="availableTags" @close="closeModal"
                 @submit="handleSubmit"/>
    </div>

    <!-- History Modal -->
    <div v-if="showHistoryModal" class="relative z-10">
      <HistoryModal :proxy-id="selectedProxyId" @close="closeHistoryModal"/>
    </div>
  </div>
</template>

<script setup>
  import { computed, onMounted, ref } from 'vue'
  import axios from 'axios'
  import TagsInput from '@/components/TagsInput.vue'
  import Pagination from '@/components/Pagination.vue'
  import ProxiesList from '@/components/ProxiesList.vue'
  import EditModal from '@/components/EditModal.vue'
  import HistoryModal from '@/components/HistoryModal.vue'

  const proxies = ref([])
  const totalProxies = ref(0)
  const currentPage = ref(1)
  const itemsPerPage = ref(10)
  const sortBy = ref('id')
  const sortDesc = ref(false)
  const selectedTags = ref([])
  const availableTags = ref([])
  const showModal = ref(false)
  const showHistoryModal = ref(false)
  const editingProxy = ref(null)
  const selectedProxyId = ref(null)

  const form = ref({
    name: '',
    listen_url: '',
    mode: 'reverse',
    tags: [],
    targets: [],
    saving_cookies_flg: false,
    query_forwarding_flg: true,
    cookies_forwarding_flg: false,
    condition: {
      type: '',
      param_name: '',
      values: [],
      default: ''
    },
  })

  const filteredProxies = computed(() => {
    if (selectedTags.value.length === 0) return proxies.value
    return proxies.value.filter(proxy =>
        selectedTags.value.every(tag => proxy.tags?.includes(tag))
    )
  })

  async function loadProxies () {
    try {
      const response = await axios.get('/api/proxies', {
        params: {
          limit: itemsPerPage.value,
          offset: (currentPage.value - 1) * itemsPerPage.value,
          sortBy: sortBy.value,
          sortDesc: sortDesc.value
        }
      })

      proxies.value = response.data.items.map(proxy => {
        // Skip unnecessary object copying for expr condition type
        if (proxy.condition?.type === 'expr') {
          return proxy
        }

        // Only transform non-expr conditions that have values
        if (proxy.condition?.values) {
          return {
            ...proxy,
            condition: {
              ...proxy.condition,
              values: proxy.targets.map(({ id }) => proxy.condition.values[id] || '')
            }
          }
        }

        // Return original proxy if no special handling needed
        return proxy
      })

      totalProxies.value = response.data.total
    } catch (error) {
      console.error('Failed to load proxies:', error)
    }
  }

  function handleSort (column) {
    if (sortBy.value === column) {
      sortDesc.value = !sortDesc.value
    } else {
      sortBy.value = column
      sortDesc.value = false
    }
    loadProxies()
  }

  function changePage (page) {
    currentPage.value = page
    loadProxies()
  }

  async function filterProxies () {
    if (selectedTags.value.length === 0) {
      await loadProxies()
    } else {
      try {
        const response = await axios.get('/api/proxies/by-tags', {
          params: { tags: selectedTags.value.join(',') }
        })
        proxies.value = response.data.proxies || []
      } catch (error) {
        console.error('Failed to filter proxies:', error)
      }
    }
  }

  function openCreateModal () {
    editingProxy.value = null
    form.value = {
      name: '',
      listen_url: '',
      listen_urls: [''],
      mode: 'reverse',
      tags: [],
      targets: [],
      saving_cookies_flg: false,
      query_forwarding_flg: true,
      cookies_forwarding_flg: false,
      condition: {
        type: '',
        param_name: '',
        values: [],
        default: ''
      }
    }
    showModal.value = true
  }

  function editProxy (proxy) {
    editingProxy.value = proxy
    form.value = {
      name: proxy.name,
      listen_url: proxy.listen_url,
      // Extract the listen_url property from each object in the listen_urls array
      // or fall back to the single listen_url if listen_urls is not available
      listen_urls: proxy.listen_urls ? proxy.listen_urls.map(url => url.listen_url) : [proxy.listen_url],
      mode: proxy.mode,
      tags: proxy.tags || [],
      saving_cookies_flg: proxy.saving_cookies_flg,
      query_forwarding_flg: proxy.query_forwarding_flg,
      cookies_forwarding_flg: proxy.cookies_forwarding_flg,
      targets: proxy.targets.map(t => ({
        id: t.id,
        name: t.name || '',
        url: t.url,
        weight: t.weight * 100,
        is_active: t.is_active
      })),
      condition: proxy.condition || {
        type: '',
        param_name: '',
        values: [],
        default: ''
      }
    }
    showModal.value = true
  }

  function closeModal () {
    showModal.value = false
    editingProxy.value = null
    form.value = {
      name: '',
      listen_url: '',
      mode: 'reverse',
      tags: [],
      targets: [],
      saving_cookies_flg: false,
      query_forwarding_flg: true,
      cookies_forwarding_flg: false,
      condition: {
        type: '',
        param_name: '',
        values: [],
        default: ''
      },
    }
  }

  function viewHistory (proxy) {
    selectedProxyId.value = proxy.id
    showHistoryModal.value = true
  }

  function closeHistoryModal () {
    showHistoryModal.value = false
    selectedProxyId.value = null
  }

  async function handleSubmit (formData) {
    try {
      if (editingProxy.value) {
        // Update URL/path key if they changed
        const currentListenUrls = editingProxy.value.listen_urls
            ? editingProxy.value.listen_urls.map(url => url.listen_url)
            : [editingProxy.value.listen_url]

        const listenUrlsChanged = JSON.stringify(formData.listen_urls) !== JSON.stringify(currentListenUrls)
        const pathKeyChanged = formData.path_key !== editingProxy.value.path_key

        if (listenUrlsChanged || pathKeyChanged) {
          await axios.put(`/api/proxies/${editingProxy.value.id}/url`, {
            listen_url: formData.listen_url, // Keep for backward compatibility
            listen_urls: formData.listen_urls,
            path_key: formData.path_key
          })
        }

        // Update saving_cookies_flg if it changed
        if (formData.saving_cookies_flg !== editingProxy.value.saving_cookies_flg) {
          await axios.put(`/api/proxies/${editingProxy.value.id}/cookies`, {
            saving_cookies_flg: formData.saving_cookies_flg
          })
        }

        // Update query_forwarding_flg if it changed
        if (formData.query_forwarding_flg !== editingProxy.value.query_forwarding_flg) {
          await axios.put(`/api/proxies/${editingProxy.value.id}/query-forwarding`, {
            query_forwarding_flg: formData.query_forwarding_flg
          })
        }

        // Update cookies_forwarding_flg if it changed
        if (formData.cookies_forwarding_flg !== editingProxy.value.cookies_forwarding_flg) {
          await axios.put(`/api/proxies/${editingProxy.value.id}/cookies-forwarding`, {
            cookies_forwarding_flg: formData.cookies_forwarding_flg
          })
        }

        // Check if condition has changed
        const conditionChanged = (
            formData.condition?.type !== editingProxy.value.condition?.type ||
            formData.condition?.param_name !== editingProxy.value.condition?.param_name ||
            formData.condition?.default !== editingProxy.value.condition?.default ||
            JSON.stringify(formData.condition?.values) !== JSON.stringify(editingProxy.value.condition?.values) ||
            formData.condition?.expr !== editingProxy.value.condition?.expr
        )

        // Update condition if it changed
        if (conditionChanged) {
          await axios.put(`/api/proxies/${editingProxy.value.id}/condition`, {
            condition: formData.condition?.type ? formData.condition : null
          });
        }

        // Update targets and tags
        await Promise.all([
          axios.put(`/api/proxies/${editingProxy.value.id}/targets`, { targets: formData.targets }),
          axios.put(`/api/proxies/${editingProxy.value.id}/tags`, { tags: formData.tags })
        ])
      } else {
        await axios.post('/api/proxies', formData)
      }

      await loadProxies()
      closeModal()
    } catch (error) {
      console.error('Failed to save proxy:', error)
      alert(error.response?.data?.error || 'Failed to save proxy')
    }
  }

  async function deleteProxy (id) {
    if (!confirm('Are you sure you want to delete this proxy?')) return

    try {
      await axios.delete(`/api/proxies/${id}`)
      await loadProxies()
    } catch (error) {
      console.error('Failed to delete proxy:', error)
    }
  }

  onMounted(async () => {
    await loadProxies()
    const tagsResponse = await axios.get('/api/tags')
    availableTags.value = tagsResponse.data.tags || []
  })
</script>
