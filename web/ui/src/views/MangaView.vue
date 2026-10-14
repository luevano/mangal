<script setup lang="ts">
import { useRoute } from 'vue-router';
import client from '@/api/client';
import { computed, onMounted, ref } from 'vue';
import type { MangaPage } from '@/api/schemas';
import { imageURL } from '@/api/image';

const route = useRoute()
const error = ref("")
const loading = ref(true)
const data = ref(null as MangaPage | null)

// the AniList description is HTML from the site: shown as text, never as markup
const description = computed(() => {
    const html = data.value?.anilistManga?.description
    if (!html) {
        return ""
    }
    // AniList puts a <br> before its newlines: one break each, at most a blank line between
    const doc = new DOMParser().parseFromString(html.replace(/\s*<br\s*\/?>\s*/gi, "\n"), "text/html")
    return (doc.body.textContent ?? "").replace(/\n{3,}/g, "\n\n").trim()
})

// the AniList cover, else the provider's (it may need the site as Referer)
const cover = computed(() => {
    const anilist = data.value?.anilistManga?.coverImage.extraLarge
    if (anilist) {
        return imageURL(anilist)
    }
    return imageURL(data.value?.manga.cover, data.value?.manga.url)
})

onMounted(async () => {
    loading.value = true

    const query = route.params['query'] as string
    const provider = route.params['provider'] as string
    const manga = route.params['manga'] as string

    const res = await client.GET('/mangaPage', {
        params: {
            query: { provider, query, manga }
        }
    })
    loading.value = false

    if (res.error) {
        error.value = res.error.message
        return
    }

    data.value = res.data
})
</script>

<template>
    <div v-if="loading" class="d-flex justify-content-center">
        <div class="spinner-border" role="status">
            <span class="visually-hidden">Loading...</span>
        </div>
    </div>
    <p v-else-if="error">{{ error }}</p>
    <div v-else>
        <img v-if="cover" :src="cover" alt="Cover image">
        <h2>{{ data?.manga.title }}</h2>
        <p class="description">{{ description }}</p>
        <ul v-for="volume in data?.volumes">
            <li>Vol. {{ volume.volume.number }}</li>
            <ul v-for="chapter in volume.chapters">
                <li>Chapter {{ chapter.number }}</li>
            </ul>
        </ul>
    </div>
</template>

<style scoped>
.description {
    white-space: pre-line;
}
</style>
