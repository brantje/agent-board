import { defineComponent, h, reactive, ref } from 'vue'
export const uiStubs = {
  PageFrame: {props:['title','description'],template:'<main><h1>{{title}}</h1><p>{{description}}</p><slot name="actions"/><slot/></main>'},
  AsyncState: {props:['pending','error','empty','emptyTitle','emptyDescription'],emits:['retry'],template:'<div><p v-if="pending">Loading</p><p v-else-if="error">{{error.message}}<button @click="$emit(\'retry\')">Retry</button></p><p v-else-if="empty">{{emptyTitle}} {{emptyDescription}}</p><slot v-else/></div>'},
  UCard: {template:'<section><slot name="header"/><slot/><slot name="footer"/></section>'},
  UButton: {props:['label','to','disabled','loading','type'],emits:['click'],template:'<button :type="type || \'button\'" :disabled="disabled || loading" @click="$emit(\'click\')">{{label}}<slot/></button>'},
  UAlert: {props:['title','description'],template:'<div role="alert">{{title}} {{description}}</div>'},
  UBadge: {props:['label','icon','color','variant','size','trailingIcon'],template:'<span :data-color="color" :data-variant="variant" :data-icon="icon">{{label}}<slot/><slot name="trailing"/></span>'},
  UAvatar: {props:['alt','text','size'],template:'<span :aria-label="alt">{{text || alt}}</span>'},
  UModal: {props:['open','title'],template:'<div v-if="open" role="dialog"><h2>{{title}}</h2><slot name="body"/></div>'},
  UForm: {props:['state','validate'],emits:['submit'],template:'<form @submit.prevent="$emit(\'submit\')"><slot/></form>'},
  UAuthForm: defineComponent({
    props: ['fields', 'title', 'description', 'submit', 'loading'],
    emits: ['submit'],
    setup(props, { emit, slots }) {
      const state = reactive<Record<string, unknown>>({})
      return () => h('form', {
        onSubmit: (event: Event) => {
          event.preventDefault()
          emit('submit', { data: { ...state } })
        }
      }, [
        h('h2', String(props.title ?? '')),
        h('p', String(props.description ?? '')),
        slots.validation?.(),
        ...((props.fields ?? []) as Array<{ name: string, type?: string, label?: string, description?: string }>).map((field) => h('label', { 'data-field': field.name }, [
          field.label ?? field.name,
          h('input', {
            type: field.type === 'checkbox' ? 'checkbox' : (field.type ?? 'text'),
            checked: field.type === 'checkbox' ? state[field.name] === true : undefined,
            value: field.type === 'checkbox' ? undefined : String(state[field.name] ?? ''),
            onInput: field.type === 'checkbox' ? undefined : (event: Event) => { state[field.name] = (event.target as HTMLInputElement).value },
            onChange: field.type === 'checkbox' ? (event: Event) => { state[field.name] = (event.target as HTMLInputElement).checked } : undefined
          }),
          field.description ? h('small', field.description) : null
        ])),
        h('button', { type: 'submit', disabled: props.loading === true }, String((props.submit as { label?: string } | undefined)?.label ?? 'Continue'))
      ])
    }
  }),
  UFormField: {props:['name','label','description'],template:'<label :data-field="name">{{label}}<slot/><small>{{description}}</small></label>'},
  UInput: {props:['modelValue','type','disabled'],emits:['update:modelValue'],template:'<input :type="type || \'text\'" :value="modelValue" :disabled="disabled" @input="$emit(\'update:modelValue\', $event.target.value)"/>'},
  UTextarea: {props:['modelValue','disabled'],emits:['update:modelValue'],template:'<textarea :value="modelValue" :disabled="disabled" @input="$emit(\'update:modelValue\', $event.target.value)"/>'},
  UCheckbox: {props:['modelValue','disabled'],emits:['update:modelValue'],template:'<input type="checkbox" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.checked)"/>'},
  USwitch: {props:['modelValue','disabled'],emits:['update:modelValue'],template:'<input type="checkbox" role="switch" :checked="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.checked)"/>'},
  USelect: {props:['modelValue','items','disabled','valueKey'],emits:['update:modelValue'],template:'<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option value="">Select…</option><option v-for="item in items" :value="typeof item === \'object\' ? item.value : item" :disabled="item.disabled">{{typeof item === \'object\' ? item.label : item}}</option></select>'},
  USelectMenu: {props:['modelValue','items','disabled','valueKey'],emits:['update:modelValue'],template:'<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option value="">Select…</option><option v-for="item in items" :value="typeof item === \'object\' ? item.value : item" :disabled="item.disabled">{{typeof item === \'object\' ? item.label : item}}</option></select>'},
  UAccordion: defineComponent({props:['items'],setup(props,{slots}) { return ()=> h('div',(props.items as {slot?:string;label?:string}[]).map(item=>h('section',[h('h3',item.label), item.slot ? slots[item.slot]?.({item}) : slots.body?.({item})]))) }}),
  UTimeline: {props:['items'],template:'<ol><li v-for="item in items" :key="item.value"><h4>{{item.title}}</h4><time>{{item.date}}</time><p>{{item.description}}</p><slot :name="(item.slot || \'item\') + \'-description\'" :item="item"/></li></ol>'},
  URadioGroup: {props:['modelValue','items','disabled'],emits:['update:modelValue'],template:'<div><label v-for="item in items" :key="item.value"><input type="radio" :value="item.value" :checked="modelValue===item.value" :disabled="disabled" @change="$emit(\'update:modelValue\', item.value)"/>{{item.label}}</label></div>'},
  UEmpty: {props:['title','description'],template:'<div>{{title}} {{description}}</div>'},
  UIcon: {props:['name'],template:'<span :class="name" :data-icon="name" />'},
  UTooltip: {props:['text'],template:'<span :title="text"><slot/></span>'},
  UProgress: {props:['modelValue','max'],template:'<progress :value="modelValue" :max="max || 100" />'},
  UCollapsible: defineComponent({
    props: ['disabled'],
    setup(props, { slots }) {
      const open = ref(false)
      return () => h('div', {
        'data-collapsible': open.value ? 'open' : 'closed',
        onClick: () => { if (props.disabled !== true) open.value = !open.value }
      }, [slots.default?.({ open: open.value }), open.value ? slots.content?.() : null])
    }
  }),
  UNavigationMenu: {props:['items'],template:'<nav><a v-for="item in items" :href="item.to">{{item.label}}</a></nav>'}
}
