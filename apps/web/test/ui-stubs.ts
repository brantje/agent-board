import { defineComponent, h } from 'vue'
export const uiStubs = {
  PageFrame: {props:['title','description'],template:'<main><h1>{{title}}</h1><p>{{description}}</p><slot name="actions"/><slot/></main>'},
  AsyncState: {props:['pending','error','empty','emptyTitle'],emits:['retry'],template:'<div><p v-if="pending">Loading</p><p v-else-if="error">{{error.message}}<button @click="$emit(\'retry\')">Retry</button></p><p v-else-if="empty">{{emptyTitle}}</p><slot v-else/></div>'},
  UCard: {template:'<section><slot name="header"/><slot/><slot name="footer"/></section>'},
  UButton: {props:['label','to','disabled','loading','type'],emits:['click'],template:'<button :type="type || \'button\'" :disabled="disabled || loading" @click="$emit(\'click\')">{{label}}<slot/></button>'},
  UAlert: {props:['title','description'],template:'<div role="alert">{{title}} {{description}}</div>'},
  UBadge: {props:['label'],template:'<span>{{label}}<slot/></span>'},
  UModal: {props:['open','title'],template:'<div v-if="open" role="dialog"><h2>{{title}}</h2><slot name="body"/></div>'},
  UForm: {props:['state','validate'],emits:['submit'],template:'<form @submit.prevent="$emit(\'submit\')"><slot/></form>'},
  UFormField: {props:['name','label','description'],template:'<label :data-field="name">{{label}}<slot/><small>{{description}}</small></label>'},
  UInput: {props:['modelValue','type','disabled'],emits:['update:modelValue'],template:'<input :type="type || \'text\'" :value="modelValue" :disabled="disabled" @input="$emit(\'update:modelValue\', $event.target.value)"/>'},
  UTextarea: {props:['modelValue','disabled'],emits:['update:modelValue'],template:'<textarea :value="modelValue" :disabled="disabled" @input="$emit(\'update:modelValue\', $event.target.value)"/>'},
  UCheckbox: {props:['modelValue','disabled'],emits:['update:modelValue'],template:'<input type="checkbox" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.checked)"/>'},
  USelect: {props:['modelValue','items','disabled'],emits:['update:modelValue'],template:'<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option value="">Select…</option><option v-for="item in items" :value="typeof item === \'object\' ? item.value : item" :disabled="item.disabled">{{typeof item === \'object\' ? item.label : item}}</option></select>'},
  UAccordion: defineComponent({props:['items'],setup(props,{slots}) { return ()=> h('div',(props.items as {slot?:string;label?:string}[]).map(item=>h('section',[h('h3',item.label), item.slot ? slots[item.slot]?.({item}) : slots.body?.({item})]))) }}),
  UNavigationMenu: {props:['items'],template:'<nav><a v-for="item in items" :href="item.to">{{item.label}}</a></nav>'}
}
