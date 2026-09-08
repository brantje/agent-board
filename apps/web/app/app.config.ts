export default defineAppConfig({
  ui: {
    colors: { primary: 'steel', neutral: 'neutral' },
    button: { slots: { base: 'rounded-xs' }, defaultVariants: { size: 'sm' } },
    input: { slots: { base: 'rounded-xs' }, defaultVariants: { size: 'sm' } },
    textarea: { slots: { base: 'rounded-xs' } },
    select: { slots: { base: 'rounded-xs', content: 'rounded-xs shadow-none' } },
    card: { slots: { root: 'rounded-none shadow-none', header: 'p-3 sm:p-3', body: 'p-3 sm:p-3', footer: 'p-3 sm:p-3' } },
    modal: { slots: { content: 'rounded-none shadow-none' } },
    slideover: { slots: { content: 'rounded-none shadow-none' } },
    alert: { slots: { root: 'rounded-none' } },
    empty: { slots: { root: 'rounded-none' } },
    navigationMenu: { slots: { link: 'rounded-xs' } },
    dashboardNavbar: { slots: { root: 'h-12 min-h-12', title: 'text-sm font-semibold' } },
    dashboardSidebar: { slots: { root: 'bg-sidebar', header: 'h-12 min-h-12', footer: 'border-t border-default' } }
  }
})
