# DeployStack - Cost Sentry 

This is a simple solution that uses a Cloud Function to enforce Billing Budget constraints.
Namely when you go over budget, any Compute Engine instance with a label of `costsentry`
will be `stopped` by this system. It uses Pub/Sub to be the bridge between the Budget
and Cloud Functions

![Cost Sentry architecture](/architecture.png)

## Install
You can install this application using the `Open in Google Cloud Shell` button 
below. 

<a href="https://ssh.cloud.google.com/cloudshell/editor?cloudshell_git_repo=https%3A%2F%2Fgithub.com%2FGoogleCloudPlatform%2Fdeploystack-cost-sentry&shellonly=true&cloudshell_image=gcr.io/ds-artifacts-cloudshell/deploystack_custom_image" target="_new">
        <img alt="Open in Cloud Shell" src="https://gstatic.com/cloudssh/images/open-btn.svg">
</a>

Clicking this link will take you right to the DeployStack app, running in your 
Cloud Shell environment. It will walk you through setting up your architecture.  

## Cleanup 
To remove all billing components from the project
1. Typing `deploystack uninstall`

This is not an official Google product.