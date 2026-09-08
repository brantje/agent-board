<script setup lang="ts">
import {reactive,ref} from 'vue'
import type {Issue} from '../types/api'
import {apiPath,apiRequest} from '../utils/api'
import {editableStatuses} from '../utils/issues'
const props=defineProps<{projectId:string;issue?:Issue;initialStatus?:string}>()
const emit=defineEmits<{saved:[issue:Issue];cancel:[]}>()
const state=reactive({title:props.issue?.title || '',description:props.issue?.description || '',status:props.issue?.status || props.initialStatus || 'BACKLOG'})
const saving=ref(false)
const error=ref<Error>()
const validate=()=>state.title.trim() ? [] : [{name:'title',message:'Title is required.'}]
async function save(){
 if(saving.value || validate().length) return
 saving.value=true;error.value=undefined
 try {const saved=await apiRequest<Issue>(apiPath('issues',props.projectId,props.issue?.id),{method:props.issue ? 'PATCH':'POST',body:{...state,title:state.title.trim()}});emit('saved',saved)}
 catch(failure){error.value=failure as Error}finally{saving.value=false}
}
</script>
<template><UForm :state="state" :validate="validate" class="space-y-4" @submit="save"><UAlert v-if="error" color="error" title="Unable to save Issue" :description="error.message"/><UFormField label="Title" name="title" required><UInput v-model="state.title" class="w-full" :disabled="saving" autofocus /></UFormField><UFormField label="Description" name="description"><UTextarea v-model="state.description" :rows="8" class="w-full" :disabled="saving"/></UFormField><UFormField label="Board status" name="status"><USelect v-model="state.status" :items="editableStatuses(issue?.status)" :disabled="saving"/></UFormField><p v-if="issue?.status === 'DONE'" class="text-sm text-muted">Reopen into TODO to allow another Run. Reopening does not start execution.</p><p v-if="issue?.status === 'REVIEW'" class="text-sm text-muted">Use the Review decision to approve or request changes.</p><div class="flex justify-end gap-2"><UButton label="Cancel" color="neutral" variant="outline" :disabled="saving" @click="emit('cancel')"/><UButton label="Save issue" type="submit" :loading="saving"/></div></UForm></template>
