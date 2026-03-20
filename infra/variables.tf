variable "clerk_secret_key" {
  description = "Clerk secret key for backend auth"
  type        = string
  sensitive   = true
}

variable "openai_api_key" {
  description = "OpenAI API key for Whisper/GPT"
  type        = string
  sensitive   = true
}

variable "clerk_publishable_key" {
  description = "Clerk publishable key (for frontend build)"
  type        = string
  default     = ""
}
