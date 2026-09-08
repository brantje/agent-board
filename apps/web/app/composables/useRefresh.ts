import {onMounted,onBeforeUnmount} from 'vue'
/** Refresh a read projection; never runs execution or persists browser state. */
export function useRefresh(refresh:()=>Promise<unknown>,milliseconds=10000){
 let timer:ReturnType<typeof setInterval>|undefined
 let running=false
 let disposed=false
 async function update(){if(running || disposed || document.hidden)return;running=true;try{await refresh()}finally{running=false}}
 onMounted(()=>{timer=setInterval(update,milliseconds);window.addEventListener('focus',update)})
 onBeforeUnmount(()=>{disposed=true;clearInterval(timer);window.removeEventListener('focus',update)})
}
