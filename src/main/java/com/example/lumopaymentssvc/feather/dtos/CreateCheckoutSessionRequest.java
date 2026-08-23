package com.example.lumopaymentssvc.feather.dtos;

import jakarta.validation.constraints.Email;
import jakarta.validation.constraints.NotBlank;
import lombok.AllArgsConstructor;
import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * Clerk's session JWT doesn't carry the account's email by default, so the
 * frontend sends it along explicitly (it already has it via useUser()) rather
 * than this service needing its own Clerk Backend API credentials just to
 * look it up.
 */
@Data
@AllArgsConstructor
@NoArgsConstructor
public class CreateCheckoutSessionRequest {

    @NotBlank
    @Email
    private String email;
}
