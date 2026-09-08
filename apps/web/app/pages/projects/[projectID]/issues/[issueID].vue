<script setup lang="ts">const route=useRoute()</script>
<template><IssueDetail :key="`${route.params.projectID}/${route.params.issueID}`" :project-id="String(route.params.projectID)" :issue-id="String(route.params.issueID)"/></template>
